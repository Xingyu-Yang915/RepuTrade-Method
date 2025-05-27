// Methodology.go - Hyperledger Fabric chaincode implementing the RepuTrade 
package main

import (
        "encoding/json"
        "encoding/pem"
        "encoding/hex"
        "encoding/asn1"
        "fmt"
        "crypto/sha256"
        "crypto/ecdsa"
        "crypto/x509"
        "math/big"
        "log"

        "github.com/hyperledger/fabric-contract-api-go/contractapi"
)

// Participant represents a trading participant with reputation, balance, and public key
type Participant struct {
        ID         string `json:"id"`
        Reputation int    `json:"reputation"`
        Balance    int    `json:"balance"`
        PublicKey  string `json:"publicKey"`
}

// Order represents a BUY or SELL order in the market
type Order struct {
        OrderID       string `json:"orderID"`
        ParticipantID string `json:"participantID"`
        OrderType     string `json:"orderType"`   // "BUY" or "SELL"
        EnergyAmount  int    `json:"energyAmount"`// energy quantity for trade
        Price         int    `json:"price"`       // price per unit energy
}

// EnergyToken represents a transaction token (trade) with deposits and signatures
type EnergyToken struct {
        TokenID          string `json:"tokenID"`
        BuyerID          string `json:"buyerID"`
        SellerID         string `json:"sellerID"`
        EnergyAmount     int    `json:"energyAmount"`
        Price            int    `json:"price"`
        Timestamp        int64  `json:"timestamp"`
        State            string `json:"state"`            // "CREATED", "LOCKED", "SUCCESS", or "DEFAULT"
        BuyerDeposit     int    `json:"buyerDeposit"`
        SellerDeposit    int    `json:"sellerDeposit"`
        BuyerReputation  int    `json:"buyerReputation"`
        SellerReputation int    `json:"sellerReputation"`
        BuyerSignature   string `json:"buyerSignature"`
        SellerSignature  string `json:"sellerSignature"`
        BuyerPaid        bool   `json:"buyerPaid"`        // whether buyer's payment confirmed
        SellerDelivered  bool   `json:"sellerDelivered"`  // whether seller's energy delivery confirmed
}

// SmartContract provides functions for managing the RepuTrade chaincode
type SmartContract struct {
        contractapi.Contract
}

// Key prefixes and constants
const participantPrefix = "PARTICIPANT_"
const orderPrefix = "ORDER_"
const tokenPrefix = "TOKEN_"
const orderCountKey = "ORDERCOUNT"
const tokenCountKey = "TOKENCOUNT"
const reputationThreshold = 20    // reputation threshold for order matching filter
const maxReputation = 100         // maximum reputation score
const minDepositPercent = 5       // minimum deposit ratio (5%)
const maxDepositPercent = 20      // maximum deposit ratio (20%)

// calculateDepositPercent computes deposit percentage based on reputation (higher rep -> lower deposit)
func calculateDepositPercent(rep int) int {
        if rep < 0 {
                rep = 0
        }
        if rep > maxReputation {
                rep = maxReputation
        }
        if rep < reputationThreshold {
                // For rep below threshold (e.g., < 20), treat as threshold
                rep = reputationThreshold
        }
        // Linear interpolation: rep 20 -> 20%, rep 100 -> 5%
        percent := 20 - (rep - 20) * 15 / 80
        if percent < 5 {
                percent = 5
        }
        if percent > 20 {
                percent = 20
        }
        return percent
}

// helper functions to form state keys
func participantKey(id string) string { return participantPrefix + id }
func orderKey(id string) string       { return orderPrefix + id }
func tokenKey(id string) string       { return tokenPrefix + id }

// ParticipantExists checks if a participant with given ID exists in the ledger
func (s *SmartContract) ParticipantExists(ctx contractapi.TransactionContextInterface, id string) (bool, error) {
        data, err := ctx.GetStub().GetState(participantKey(id))
        if err != nil {
                return false, fmt.Errorf("failed to read participant: %v", err)
        }
        return data != nil, nil
}

// OrderExists checks if an order with given ID exists in the ledger
func (s *SmartContract) OrderExists(ctx contractapi.TransactionContextInterface, id string) (bool, error) {
        data, err := ctx.GetStub().GetState(orderKey(id))
        if err != nil {
                return false, fmt.Errorf("failed to read order: %v", err)
        }
        return data != nil, nil
}

// InitLedger initializes the ledger (setting initial counters for orders and tokens)
func (s *SmartContract) InitLedger(ctx contractapi.TransactionContextInterface) error {
    // 初始化计数器为0，避免非确定性行为
    initialOrderCount := 0
    countBytes, err := json.Marshal(initialOrderCount)
    if err != nil {
        return fmt.Errorf("failed to marshal initial order count: %v", err)
    }
    if err := ctx.GetStub().PutState(orderCountKey, countBytes); err != nil {
        return fmt.Errorf("failed to set initial order count: %v", err)
    }

    initialTokenCount := 0
    tokenCountBytes, err := json.Marshal(initialTokenCount)
    if err != nil {
        return fmt.Errorf("failed to marshal initial token count: %v", err)
    }
    if err := ctx.GetStub().PutState(tokenCountKey, tokenCountBytes); err != nil {
        return fmt.Errorf("failed to set initial token count: %v", err)
    }

    return nil
}

// CreateParticipant registers a new participant with given reputation, balance, and optionally a public key.
// If pubKeyPem is empty, a new ECDSA key pair is generated for the participant.
func (s *SmartContract) CreateParticipant(ctx contractapi.TransactionContextInterface, id string, reputation int, balance int, pubKeyPem string) error {
        exists, err := s.ParticipantExists(ctx, id)
        if err != nil {
                return fmt.Errorf("failed to check participant existence: %v", err)
        }
        if exists {
                return fmt.Errorf("participant %s already exists", id)
        }

        // 检查余额是否非负
        if balance < 0 {
                return fmt.Errorf("balance cannot be negative")
        }

        // 检查公钥格式是否合法
        block, _ := pem.Decode([]byte(pubKeyPem))
        if block == nil || block.Type != "PUBLIC KEY" {
                return fmt.Errorf("invalid PEM format for public key")
        }

        pubKeyInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
        if err != nil {
                return fmt.Errorf("invalid public key data: %v", err)
        }

        _, ok := pubKeyInterface.(*ecdsa.PublicKey)
        if !ok {
                return fmt.Errorf("public key is not ECDSA format")
        }

        participant := Participant{
                ID:         id,
                Reputation: reputation,
                Balance:    balance,
                PublicKey:  pubKeyPem,
        }

        participantJSON, err := json.Marshal(participant)
        if err != nil {
                return fmt.Errorf("failed to marshal participant: %v", err)
        }

        return ctx.GetStub().PutState(participantKey(id), participantJSON)
}

// CreateOrder places a new BUY or SELL order for a participant with specified energy amount and price.
func (s *SmartContract) CreateOrder(ctx contractapi.TransactionContextInterface, id string, participantID string, energyAmount int, price int, orderType string) error {
        exists, err := s.OrderExists(ctx, id)
        if err != nil {
                return fmt.Errorf("failed to check order existence: %v", err)
        }
        if exists {
                return fmt.Errorf("order %s already exists", id)
        }

        if orderType != "BUY" && orderType != "SELL" {
                return fmt.Errorf("order type must be either BUY or SELL")
        }

        if energyAmount <= 0 || price <= 0 {
                return fmt.Errorf("energy amount and price must be positive integers")
        }

        participantJSON, err := ctx.GetStub().GetState(participantKey(participantID))
        if err != nil {
                return fmt.Errorf("failed to get participant: %v", err)
        }
        if participantJSON == nil {
                return fmt.Errorf("participant %s does not exist", participantID)
        }

        var participant Participant
        err = json.Unmarshal(participantJSON, &participant)
        if err != nil {
                return fmt.Errorf("failed to unmarshal participant: %v", err)
        }

        // 检查信誉值，最低允许为20
        if participant.Reputation < reputationThreshold {
                return fmt.Errorf("participant reputation (%d) too low to create order (minimum required is %d)", participant.Reputation, reputationThreshold)
        }

        // 根据信誉值线性计算押金比例（20分对应20%，100分对应5%）
        depPercent := calculateDepositPercent(participant.Reputation)
        minDeposit := (energyAmount * price * depPercent) / 100

        if participant.Balance < minDeposit {
                return fmt.Errorf("insufficient balance to create order, required deposit: %d, current balance: %d", minDeposit, participant.Balance)
        }

        order := Order{
                OrderID:       id,
                ParticipantID: participantID,
                OrderType:     orderType,
                EnergyAmount:  energyAmount,
                Price:         price,
        }

        orderJSON, err := json.Marshal(order)
        if err != nil {
                return fmt.Errorf("failed to marshal order: %v", err)
        }

        return ctx.GetStub().PutState(orderKey(id), orderJSON)
}

// PerformMarketMatching executes the reputation-driven matching algorithm to match BUY and SELL orders.
// It filters out orders from low-reputation participants, sorts remaining orders by price, 
// and matches them iteratively. For each match, an EnergyToken is issued and relevant orders are updated or removed.
func (sc *SmartContract) PerformMarketMatching(ctx contractapi.TransactionContextInterface) error {
        // Retrieve all orders from state
        iter, err := ctx.GetStub().GetStateByRange(orderPrefix, orderPrefix+"~")
        if err != nil {
                return fmt.Errorf("failed to get orders: %v", err)
        }
        defer iter.Close()

        var buyOrders []Order
        var sellOrders []Order
        for iter.HasNext() {
                kv, err := iter.Next()
                if err != nil {
                        return fmt.Errorf("error iterating orders: %v", err)
                }
                var o Order
                if err := json.Unmarshal(kv.Value, &o); err != nil {
                        continue
                }
                // Filter out orders from participants with low reputation
                partBytes, _ := ctx.GetStub().GetState(participantKey(o.ParticipantID))
                if partBytes == nil {
                        continue
                }
                var p Participant
                _ = json.Unmarshal(partBytes, &p)
                if p.Reputation < reputationThreshold {
                        // skip orders from low-reputation participant (ignored for matching)
                        continue
                }
                if o.OrderType == "BUY" {
                        buyOrders = append(buyOrders, o)
                } else if o.OrderType == "SELL" {
                        sellOrders = append(sellOrders, o)
                }
        }
        // Sort buy orders by descending price, sell orders by ascending price
        for i := 0; i < len(buyOrders); i++ {
                for j := i + 1; j < len(buyOrders); j++ {
                        if buyOrders[j].Price > buyOrders[i].Price {
                                buyOrders[i], buyOrders[j] = buyOrders[j], buyOrders[i]
                        }
                }
        }
        for i := 0; i < len(sellOrders); i++ {
                for j := i + 1; j < len(sellOrders); j++ {
                        if sellOrders[j].Price < sellOrders[i].Price {
                                sellOrders[i], sellOrders[j] = sellOrders[j], sellOrders[i]
                        }
                }
        }
        // Match orders iteratively
        for len(buyOrders) > 0 && len(sellOrders) > 0 {
                b := &buyOrders[0]
                s := &sellOrders[0]
                // Check if highest bid meets lowest ask
                if b.Price >= s.Price {
                        // Issue a transaction token for the matched pair (no signatures provided in automated matching)
                        if err := sc.IssueToken(ctx, b.OrderID, s.OrderID, "", ""); err != nil {
                                return err
                        }
                        // Determine if orders are fully or partially matched
                        if b.EnergyAmount > s.EnergyAmount {
                                // Buyer order partially fulfilled
                                b.EnergyAmount -= s.EnergyAmount
                                // Remove the seller order (fully fulfilled) from list
                                sellOrders = sellOrders[1:]
                                // Keep the buyer order in list with updated quantity
                        } else if b.EnergyAmount < s.EnergyAmount {
                                // Seller order partially fulfilled
                                s.EnergyAmount -= b.EnergyAmount
                                // Remove the buyer order (fully fulfilled) from list
                                buyOrders = buyOrders[1:]
                                // Keep the seller order in list with updated quantity
                        } else {
                                // Exact match, remove both orders from lists
                                buyOrders = buyOrders[1:]
                                sellOrders = sellOrders[1:]
                        }
                        continue
                } else {
                        // No matchable pairs if highest bid is below lowest ask
                        break
                }
        }
        return nil
}

// IssueToken creates a new EnergyToken for a matched buy/sell order pair, locking deposits and recording a snapshot of reputations.
// Optionally, it verifies provided digital signatures (buyerSigHex, sellerSigHex) using the participants' public keys.
func (s *SmartContract) IssueToken(ctx contractapi.TransactionContextInterface, buyOrderID string, sellOrderID string, buyerSigHex string, sellerSigHex string) error {
        // Fetch the buy and sell orders from state
        buyBytes, err := ctx.GetStub().GetState(orderKey(buyOrderID))
        if err != nil {
                return fmt.Errorf("failed to read buy order: %v", err)
        }
        sellBytes, err := ctx.GetStub().GetState(orderKey(sellOrderID))
        if err != nil {
                return fmt.Errorf("failed to read sell order: %v", err)
        }
        if buyBytes == nil || sellBytes == nil {
                return fmt.Errorf("one or both order IDs not found or already matched")
        }
        var buyOrder, sellOrder Order
        _ = json.Unmarshal(buyBytes, &buyOrder)
        _ = json.Unmarshal(sellBytes, &sellOrder)
        if buyOrder.OrderType != "BUY" || sellOrder.OrderType != "SELL" {
                return fmt.Errorf("orders %s and %s are not complementary BUY/SELL", buyOrderID, sellOrderID)
        }
        // Ensure price condition is satisfied
        if buyOrder.Price < sellOrder.Price {
                return fmt.Errorf("cannot issue token: buy order price (%d) is lower than sell order price (%d)", buyOrder.Price, sellOrder.Price)
        }
        // 防止自买自卖订单成交检查
        if buyOrder.ParticipantID == sellOrder.ParticipantID {
                return fmt.Errorf("buyer and seller cannot be the same participant (%s)", buyOrder.ParticipantID)
        }

        // Determine matched quantity and trade price
        matchedQty := buyOrder.EnergyAmount
        if sellOrder.EnergyAmount < matchedQty {
                matchedQty = sellOrder.EnergyAmount
        }
        tradePrice := sellOrder.Price  // execute trade at seller's price
        // Fetch participants (buyer and seller) from state
        buyerBytes, err := ctx.GetStub().GetState(participantKey(buyOrder.ParticipantID))
        if err != nil {
                return fmt.Errorf("failed to read buyer participant: %v", err)
        }
        sellerBytes, err := ctx.GetStub().GetState(participantKey(sellOrder.ParticipantID))
        if err != nil {
                return fmt.Errorf("failed to read seller participant: %v", err)
        }
        if buyerBytes == nil || sellerBytes == nil {
                return fmt.Errorf("buyer or seller participant not found")
        }
        var buyer, seller Participant
        _ = json.Unmarshal(buyerBytes, &buyer)
        _ = json.Unmarshal(sellerBytes, &seller)
        // Calculate deposit amounts for buyer and seller based on their reputation and transaction value
        totalValue := matchedQty * tradePrice
        percentBuyer := calculateDepositPercent(buyer.Reputation)
        percentSeller := calculateDepositPercent(seller.Reputation)
        buyerDep := (percentBuyer * totalValue) / 100
        sellerDep := (percentSeller * totalValue) / 100
        // Ensure participants have sufficient balance for deposits (and buyer for potential payment)
        if buyer.Balance < buyerDep {
                return fmt.Errorf("buyer %s has insufficient balance for deposit", buyer.ID)
        }
        if seller.Balance < sellerDep {
                return fmt.Errorf("seller %s has insufficient balance for deposit", seller.ID)
        }
        // (Optional) Check buyer's balance for full payment as well (liquidity check)
        if buyer.Balance < buyerDep + totalValue {
                // Not enough balance for both deposit and full payment – proceed with deposit lock; actual payment checked at settlement
        }
        // Deduct deposit amounts from buyer and seller balances (lock in escrow)
        buyer.Balance -= buyerDep
        seller.Balance -= sellerDep
        // Update participant states with new balances
        buyerBytes, _ = json.Marshal(buyer)
        sellerBytes, _ = json.Marshal(seller)
        if err := ctx.GetStub().PutState(participantKey(buyer.ID), buyerBytes); err != nil {
                return err
        }
        if err := ctx.GetStub().PutState(participantKey(seller.ID), sellerBytes); err != nil {
                return err
        }
        // Generate a new TokenID
        countBytes, _ := ctx.GetStub().GetState(tokenCountKey)
        var tokenCount int
        if countBytes != nil {
                _ = json.Unmarshal(countBytes, &tokenCount)
        }
        tokenCount++
        tokenID := fmt.Sprintf("token%d", tokenCount)
        newCountBytes, _ := json.Marshal(tokenCount)
        ctx.GetStub().PutState(tokenCountKey, newCountBytes)
        // Timestamp for token creation
        txTime, err := ctx.GetStub().GetTxTimestamp()
        if err != nil {
                return fmt.Errorf("failed to get transaction timestamp: %v", err)
        }
        // Convert protobuf Timestamp to Unix epoch seconds
        txTimeSeconds := txTime.GetSeconds()
        token := EnergyToken{
                TokenID:          tokenID,
                BuyerID:          buyer.ID,
                SellerID:         seller.ID,
                EnergyAmount:     matchedQty,
                Price:            tradePrice,
                Timestamp:        txTimeSeconds,
                State:            "LOCKED", // deposits locked at initiation
                BuyerDeposit:     buyerDep,
                SellerDeposit:    sellerDep,
                BuyerReputation:  buyer.Reputation,
                SellerReputation: seller.Reputation,
                BuyerSignature:   "",
                SellerSignature:  "",
                BuyerPaid:        false,
                SellerDelivered:  false,
        }
        // Verify and record digital signatures if provided
        if buyerSigHex != "" {
                sigBytes, err := hex.DecodeString(buyerSigHex)
                if err != nil {
                        return fmt.Errorf("invalid buyer signature format: %v", err)
                }
                var sigStruct struct{ R, S *big.Int }
                if _, err := asn1.Unmarshal(sigBytes, &sigStruct); err != nil {
                        return fmt.Errorf("failed to parse buyer signature: %v", err)
                }
                block, _ := pem.Decode([]byte(buyer.PublicKey))
                if block == nil {
                        return fmt.Errorf("failed to decode buyer's public key PEM")
                }
                pubInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
                if err != nil {
                        return fmt.Errorf("failed to parse buyer's public key: %v", err)
                }
                pubKey, ok := pubInterface.(*ecdsa.PublicKey)
                if !ok {
                        return fmt.Errorf("buyer public key is not ECDSA")
                }
                // Use tokenID as the message to verify signature
                msgHash := sha256.Sum256([]byte(tokenID))
                if !ecdsa.Verify(pubKey, msgHash[:], sigStruct.R, sigStruct.S) {
                        return fmt.Errorf("buyer signature verification failed")
                }
                token.BuyerSignature = buyerSigHex
        }
        if sellerSigHex != "" {
                sigBytes, err := hex.DecodeString(sellerSigHex)
                if err != nil {
                        return fmt.Errorf("invalid seller signature format: %v", err)
                }
                var sigStruct struct{ R, S *big.Int }
                if _, err := asn1.Unmarshal(sigBytes, &sigStruct); err != nil {
                        return fmt.Errorf("failed to parse seller signature: %v", err)
                }
                block, _ := pem.Decode([]byte(seller.PublicKey))
                if block == nil {
                        return fmt.Errorf("failed to decode seller's public key PEM")
                }
                pubInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
                if err != nil {
                        return fmt.Errorf("failed to parse seller's public key: %v", err)
                }
                pubKey, ok := pubInterface.(*ecdsa.PublicKey)
                if !ok {
                        return fmt.Errorf("seller public key is not ECDSA")
                }
                msgHash := sha256.Sum256([]byte(tokenID))
                if !ecdsa.Verify(pubKey, msgHash[:], sigStruct.R, sigStruct.S) {
                        return fmt.Errorf("seller signature verification failed")
                }
                token.SellerSignature = sellerSigHex
        }
        // Store the new EnergyToken on the ledger
        tokenBytes, _ := json.Marshal(token)
        if err := ctx.GetStub().PutState(tokenKey(tokenID), tokenBytes); err != nil {
                return err
        }
        // Update or remove the matched orders due to this trade
        if buyOrder.EnergyAmount > sellOrder.EnergyAmount {
                // Buyer order partially fulfilled: reduce its amount and update state
                buyOrder.EnergyAmount -= sellOrder.EnergyAmount
                updatedBuyBytes, _ := json.Marshal(buyOrder)
                ctx.GetStub().PutState(orderKey(buyOrder.OrderID), updatedBuyBytes)
                // Seller order fully fulfilled: remove it from state
                ctx.GetStub().DelState(orderKey(sellOrder.OrderID))
        } else if buyOrder.EnergyAmount < sellOrder.EnergyAmount {
                // Seller order partially fulfilled: reduce its amount and update state
                sellOrder.EnergyAmount -= buyOrder.EnergyAmount
                updatedSellBytes, _ := json.Marshal(sellOrder)
                ctx.GetStub().PutState(orderKey(sellOrder.OrderID), updatedSellBytes)
                // Buyer order fully fulfilled: remove it from state
                ctx.GetStub().DelState(orderKey(buyOrder.OrderID))
        } else {
                // Both orders fully matched: remove both from state
                ctx.GetStub().DelState(orderKey(buyOrder.OrderID))
                ctx.GetStub().DelState(orderKey(sellOrder.OrderID))
        }
        return nil
}

// ProcessEnergyFlow simulates the confirmation of energy delivery for a given transaction (token).
// It marks the token as energy delivered by the seller.
func (s *SmartContract) ProcessEnergyFlow(ctx contractapi.TransactionContextInterface, tokenID string) error {
        tokenBytes, err := ctx.GetStub().GetState(tokenKey(tokenID))
        if err != nil {
                return fmt.Errorf("failed to read token: %v", err)
        }
        if tokenBytes == nil {
                return fmt.Errorf("transaction token %s not found", tokenID)
        }
        var token EnergyToken
        _ = json.Unmarshal(tokenBytes, &token)
        if token.State != "LOCKED" {
                return fmt.Errorf("token %s is not in a LOCKED state for energy delivery (current state: %s)", tokenID, token.State)
        }
        // Mark that the seller has delivered the energy (e.g., via meter data)
        token.SellerDelivered = true
        tokenBytes, _ = json.Marshal(token)
        return ctx.GetStub().PutState(tokenKey(token.TokenID), tokenBytes)
}

// ProcessCashFlow simulates the confirmation of payment for a given transaction (token).
// It marks the token as payment completed by the buyer.
func (s *SmartContract) ProcessCashFlow(ctx contractapi.TransactionContextInterface, tokenID string) error {
        tokenBytes, err := ctx.GetStub().GetState(tokenKey(tokenID))
        if err != nil {
                return fmt.Errorf("failed to read token: %v", err)
        }
        if tokenBytes == nil {
                return fmt.Errorf("transaction token %s not found", tokenID)
        }
        var token EnergyToken
        _ = json.Unmarshal(tokenBytes, &token)
        if token.State != "LOCKED" {
                return fmt.Errorf("token %s is not in a LOCKED state for cash payment (current state: %s)", tokenID, token.State)
        }
        // Mark that the buyer has made the payment
        token.BuyerPaid = true
        tokenBytes, _ = json.Marshal(token)
        return ctx.GetStub().PutState(tokenKey(token.TokenID), tokenBytes)
}

// SettleTransaction finalizes the transaction represented by the token.
// If both energy and payment are confirmed, it marks the trade SUCCESS and releases deposits (and transfers payment).
// If either party defaulted, it marks DEFAULT, penalizes the defaulter's deposit, and compensates the other party.
func (s *SmartContract) SettleTransaction(ctx contractapi.TransactionContextInterface, tokenID string) error {
        tokenBytes, err := ctx.GetStub().GetState(tokenKey(tokenID))
        if err != nil {
                return fmt.Errorf("failed to read token: %v", err)
        }
        if tokenBytes == nil {
                return fmt.Errorf("transaction token %s not found", tokenID)
        }
        var token EnergyToken
        _ = json.Unmarshal(tokenBytes, &token)
        if token.State != "LOCKED" {
                return fmt.Errorf("transaction %s is already settled (state: %s)", tokenID, token.State)
        }
        // Fetch buyer and seller participants
        buyerBytes, _ := ctx.GetStub().GetState(participantKey(token.BuyerID))
        sellerBytes, _ := ctx.GetStub().GetState(participantKey(token.SellerID))
        if buyerBytes == nil || sellerBytes == nil {
                return fmt.Errorf("participants for token %s not found", tokenID)
        }
        var buyer, seller Participant
        _ = json.Unmarshal(buyerBytes, &buyer)
        _ = json.Unmarshal(sellerBytes, &seller)
        // Determine outcome of the transaction
        if token.SellerDelivered && token.BuyerPaid {
                // Successful transaction
                token.State = "SUCCESS"
                // Transfer payment from buyer to seller
                totalValue := token.EnergyAmount * token.Price
                if buyer.Balance < totalValue {
                        // Buyer cannot pay full amount (treat as buyer default)
                        token.State = "DEFAULT"
                        // Buyer default: seller receives buyer's deposit as compensation
                        seller.Balance += token.BuyerDeposit
                        // Seller's deposit returned to seller
                        seller.Balance += token.SellerDeposit
                        // Buyer's deposit is forfeited (remains deducted)
                        token.BuyerDeposit = 0
                        token.SellerDeposit = 0
                        // Update state and participants
                        buyerBytes, _ = json.Marshal(buyer)
                        sellerBytes, _ = json.Marshal(seller)
                        ctx.GetStub().PutState(participantKey(buyer.ID), buyerBytes)
                        ctx.GetStub().PutState(participantKey(seller.ID), sellerBytes)
                        tokenBytes, _ = json.Marshal(token)
                        ctx.GetStub().PutState(tokenKey(token.TokenID), tokenBytes)
                        return nil
                }
                // Deduct payment from buyer and credit to seller
                buyer.Balance -= totalValue
                seller.Balance += totalValue
                // Return deposits to both parties
                buyer.Balance += token.BuyerDeposit
                seller.Balance += token.SellerDeposit
                // Deposits are released
                token.BuyerDeposit = 0
                token.SellerDeposit = 0
                // Update participant balances in state
                buyerBytes, _ = json.Marshal(buyer)
                sellerBytes, _ = json.Marshal(seller)
                ctx.GetStub().PutState(participantKey(buyer.ID), buyerBytes)
                ctx.GetStub().PutState(participantKey(seller.ID), sellerBytes)
        } else {
                // Default scenario (one or both did not complete obligations)
                token.State = "DEFAULT"
                if token.SellerDelivered && !token.BuyerPaid {
                        // Buyer defaulted (energy delivered, payment not made)
                        // Seller keeps buyer's deposit
                        seller.Balance += token.BuyerDeposit
                        // Seller's deposit returned to seller
                        seller.Balance += token.SellerDeposit
                } else if !token.SellerDelivered && token.BuyerPaid {
                        // Seller defaulted (payment made, energy not delivered)
                        // Buyer keeps seller's deposit
                        buyer.Balance += token.SellerDeposit
                        // Buyer's deposit returned to buyer
                        buyer.Balance += token.BuyerDeposit
                } else {
                        // Neither delivered nor paid (seller failed to deliver, buyer withheld payment)
                        // Buyer gets seller's deposit
                        buyer.Balance += token.SellerDeposit
                        // Buyer's deposit returned to buyer
                        buyer.Balance += token.BuyerDeposit
                }
                // Deducted deposits remain accounted; set to 0 in token
                token.BuyerDeposit = 0
                token.SellerDeposit = 0
                // Update participant balances
                buyerBytes, _ = json.Marshal(buyer)
                sellerBytes, _ = json.Marshal(seller)
                ctx.GetStub().PutState(participantKey(buyer.ID), buyerBytes)
                ctx.GetStub().PutState(participantKey(seller.ID), sellerBytes)
        }
        // Save updated token state
        tokenBytes, _ = json.Marshal(token)
        return ctx.GetStub().PutState(tokenKey(token.TokenID), tokenBytes)
}

// UpdateReputationScores updates the reputation scores of the buyer and seller after a transaction is settled.
// On success, both parties' reputation may increase. On default, the defaulter's reputation is significantly decreased.
func (s *SmartContract) UpdateReputationScores(ctx contractapi.TransactionContextInterface, tokenID string) error {
        tokenBytes, err := ctx.GetStub().GetState(tokenKey(tokenID))
        if err != nil {
                return fmt.Errorf("failed to read token: %v", err)
        }
        if tokenBytes == nil {
                return fmt.Errorf("transaction token %s not found", tokenID)
        }

        var token EnergyToken
        if err := json.Unmarshal(tokenBytes, &token); err != nil {
                return fmt.Errorf("failed to unmarshal token: %v", err)
        }

        if token.State != "SUCCESS" && token.State != "DEFAULT" {
                return fmt.Errorf("transaction %s is not settled yet (state: %s)", tokenID, token.State)
        }

        buyerBytes, err := ctx.GetStub().GetState(participantKey(token.BuyerID))
        if err != nil || buyerBytes == nil {
                return fmt.Errorf("buyer %s not found", token.BuyerID)
        }
        var buyer Participant
        if err := json.Unmarshal(buyerBytes, &buyer); err != nil {
                return fmt.Errorf("failed to unmarshal buyer: %v", err)
        }

        sellerBytes, err := ctx.GetStub().GetState(participantKey(token.SellerID))
        if err != nil || sellerBytes == nil {
                return fmt.Errorf("seller %s not found", token.SellerID)
        }
        var seller Participant
        if err := json.Unmarshal(sellerBytes, &seller); err != nil {
                return fmt.Errorf("failed to unmarshal seller: %v", err)
        }

        const maxReputation = 100
        const penalty = 5

        switch token.State {
        case "SUCCESS":
                // Successful trade: increase both reputations by 1 (up to maxReputation)
                if buyer.Reputation < maxReputation {
                        buyer.Reputation++
                }
                if seller.Reputation < maxReputation {
                        seller.Reputation++
                }
        case "DEFAULT":
                // Default: penalize the defaulter's reputation
                if token.SellerDelivered && !token.BuyerPaid {
                        // Buyer defaulted on payment
                        buyer.Reputation -= penalty
                        if buyer.Reputation < 0 {
                                buyer.Reputation = 0
                        }
                } else if !token.SellerDelivered && token.BuyerPaid {
                        // Seller defaulted on delivery
                        seller.Reputation -= penalty
                        if seller.Reputation < 0 {
                                seller.Reputation = 0
                        }
                } else {
                        // Neither delivered nor paid (treat as seller default)
                        seller.Reputation -= penalty
                        if seller.Reputation < 0 {
                                seller.Reputation = 0
                        }
                }
        }

        // Update participants' reputation in ledger
        buyerBytes, err = json.Marshal(buyer)
        if err != nil {
                return fmt.Errorf("failed to marshal updated buyer: %v", err)
        }
        if err := ctx.GetStub().PutState(participantKey(buyer.ID), buyerBytes); err != nil {
                return fmt.Errorf("failed to update buyer state: %v", err)
        }

        sellerBytes, err = json.Marshal(seller)
        if err != nil {
                return fmt.Errorf("failed to marshal updated seller: %v", err)
        }
        if err := ctx.GetStub().PutState(participantKey(seller.ID), sellerBytes); err != nil {
                return fmt.Errorf("failed to update seller state: %v", err)
        }

        return nil
}

// QueryReputation returns the reputation of a participant by ID
func (s *SmartContract) QueryReputation(ctx contractapi.TransactionContextInterface, participantID string) (int, error) {
        participantJSON, err := ctx.GetStub().GetState(participantKey(participantID))
        if err != nil {
                return 0, fmt.Errorf("failed to get participant: %v", err)
        }
        if participantJSON == nil {
                return 0, fmt.Errorf("participant %s does not exist", participantID)
        }

        var participant Participant
        if err := json.Unmarshal(participantJSON, &participant); err != nil {
                return 0, fmt.Errorf("failed to unmarshal participant: %v", err)
        }

        return participant.Reputation, nil
}

// VerifySignature verifies an ECDSA signature (in hex format) on a given message using the participant's public key.
// Returns true if the signature is valid, false otherwise.
func (s *SmartContract) VerifySignature(ctx contractapi.TransactionContextInterface, participantID string, message string, signatureHex string) (bool, error) {
        partBytes, err := ctx.GetStub().GetState(participantKey(participantID))
        if err != nil {
                return false, fmt.Errorf("failed to read participant: %v", err)
        }
        if partBytes == nil {
                return false, fmt.Errorf("participant %s not found", participantID)
        }
        var participant Participant
        _ = json.Unmarshal(partBytes, &participant)
        // Decode participant's stored public key (PEM)
        block, _ := pem.Decode([]byte(participant.PublicKey))
        if block == nil {
                return false, fmt.Errorf("failed to decode public key for participant %s", participantID)
        }
        pubInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
        if err != nil {
                return false, fmt.Errorf("failed to parse public key: %v", err)
        }
        pubKey, ok := pubInterface.(*ecdsa.PublicKey)
        if !ok {
                return false, fmt.Errorf("public key is not ECDSA")
        }
        // Decode signature hex to bytes and parse ASN.1 to r and s
        sigBytes, err := hex.DecodeString(signatureHex)
        if err != nil {
                return false, fmt.Errorf("invalid signature format: %v", err)
        }
        var sigStruct struct{ R, S *big.Int }
        if _, err := asn1.Unmarshal(sigBytes, &sigStruct); err != nil {
                return false, fmt.Errorf("failed to parse signature: %v", err)
        }
        // Compute hash of the message
        hash := sha256.Sum256([]byte(message))
        valid := ecdsa.Verify(pubKey, hash[:], sigStruct.R, sigStruct.S)

        return valid, nil
}

// GetParticipant returns the Participant struct for a given participant ID
func (s *SmartContract) GetParticipant(ctx contractapi.TransactionContextInterface, id string) (*Participant, error) {
        data, err := ctx.GetStub().GetState(participantKey(id))
        if err != nil {
                return nil, fmt.Errorf("failed to read participant: %v", err)
        }
        if data == nil {
                return nil, fmt.Errorf("participant %s does not exist", id)
        }
        var participant Participant
        _ = json.Unmarshal(data, &participant)
        return &participant, nil
}

// GetOrder returns the Order struct for a given order ID
func (s *SmartContract) GetOrder(ctx contractapi.TransactionContextInterface, orderID string) (*Order, error) {
        data, err := ctx.GetStub().GetState(orderKey(orderID))
        if err != nil {
                return nil, fmt.Errorf("failed to read order: %v", err)
        }
        if data == nil {
                return nil, fmt.Errorf("order %s does not exist", orderID)
        }
        var order Order
        _ = json.Unmarshal(data, &order)
        return &order, nil
}

// GetEnergyToken returns the EnergyToken struct for a given token ID (transaction record)
func (s *SmartContract) GetEnergyToken(ctx contractapi.TransactionContextInterface, tokenID string) (*EnergyToken, error) {
        data, err := ctx.GetStub().GetState(tokenKey(tokenID))
        if err != nil {
                return nil, fmt.Errorf("failed to read token: %v", err)
        }
        if data == nil {
                return nil, fmt.Errorf("token %s does not exist", tokenID)
        }
        var token EnergyToken
        _ = json.Unmarshal(data, &token)
        return &token, nil
}

func main() {
        chaincode, err := contractapi.NewChaincode(new(SmartContract))
        if err != nil {
                log.Panicf("Error creating RepuTrade chaincode: %v", err)
        }
        if err := chaincode.Start(); err != nil {
                log.Panicf("Error starting RepuTrade chaincode: %v", err)
        }
}
