import os
import subprocess
import random
import time
import pandas as pd
import matplotlib.pyplot as plt

# 配置参数
CHANNEL_NAME = "mychannel"          # Fabric通道名称（确保已在环境中设置）
CHAINCODE_NAME = "energyTrading"    # 链码名称（根据实际部署的链码名称调整）
USER_COUNT = 100                   # 用户数量
ROUND_COUNT = 4                    # 交易轮数（每轮间隔30分钟，总计2小时4轮）
DEFAULT_PROB = 0.05                # 每笔交易违约概率5%
REPUTATION_THRESHOLD = 20          # 
os.chdir(os.path.dirname(os.path.abspath(__file__)))
# 使用Org1管理员身份执行所有操作 - 设置身份和TLS相关环境变量
os.environ["CORE_PEER_LOCALMSPID"] = "Org1MSP"
os.environ["CORE_PEER_TLS_ROOTCERT_FILE"] = "/home/xingyu/Desktop/fabric-samples/test-network/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt"
os.environ["CORE_PEER_MSPCONFIGPATH"] = "/home/xingyu/Desktop/fabric-samples/test-network/organizations/peerOrganizations/org1.example.com/users/Admin@org1.example.com/msp"
os.environ["CORE_PEER_ADDRESS"] = "localhost:7051"
os.environ["CORE_PEER_TLS_ENABLED"] = "true"
# 初始化用户信誉值：均匀随机分布在40~60
users = [f"user{uid}" for uid in range(1, USER_COUNT+1)]
reputation = {user: random.randint(40, 60) for user in users}

# 准备数据收集的数据结构
rep_history = []      # 记录每轮后每个用户的信誉值
default_records = []  # 记录每笔违约交易（用户及轮次）
round_stats = []      # 每轮汇总统计

# 打印初始化信息
print(f"Initialized {USER_COUNT} users with reputation 40~60. Starting simulation...")

# 逐轮模拟交易
for rnd in range(1, ROUND_COUNT+1):
    print(f"\n=== Round {rnd} ===")
    # 1. 生成本轮订单并调用链码记录订单创建
    orders = []  # 保存本轮有效订单的信息
    for user in users:
        # 仅信誉不低于阈值的用户参与本轮交易
        if reputation[user] >= REPUTATION_THRESHOLD:
            order_type = random.choice(["BUY", "SELL"])   # 0表示BUY，1表示SELL
            order_id = f"ORD{rnd}_{user}"  # 构造订单ID（例如 "ORD1_user5" 表示第1轮user5的订单）
            orders.append({"order_id": order_id, "user": user, "type": order_type})
            # 调用链码创建订单
            energy_amount = random.randint(1, 10)
            price = random.randint(50, 100)
            invoke_cmd = (
	        f'peer chaincode invoke '
	        f'-o localhost:7050 '
	        f'--ordererTLSHostnameOverride orderer.example.com '
	        f'--tls --cafile /home/xingyu/Desktop/fabric-samples/test-network/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem '
	        f'-C {CHANNEL_NAME} -n {CHAINCODE_NAME} '
	        f'--peerAddresses localhost:7051 '
	        f'--tlsRootCertFiles /home/xingyu/Desktop/fabric-samples/test-network/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt '
	        f'--peerAddresses localhost:9051 '
	        f'--tlsRootCertFiles /home/xingyu/Desktop/fabric-samples/test-network/organizations/peerOrganizations/org2.example.com/peers/peer0.org2.example.com/tls/ca.crt '
	        f'-c \'{{"Args":["CreateOrder","{order_id}","{user}","{energy_amount}","{price}","{order_type}"]}}\' '
	    '--waitForEvent'
	   )

            result = subprocess.run(invoke_cmd, shell=True, capture_output=True, text=True)
            if result.returncode != 0:
                print(f"CreateOrder invoke failed for {order_id}: {result.stderr}")
        else:
            # 该用户信誉过低，被排除本轮交易
            pass

    print(f"Orders placed: {len(orders)} (excluded users: {USER_COUNT - len(orders)})")

    # 2. 撮合买单和卖单
    buy_orders = [o for o in orders if o["type"] == "BUY"]
    sell_orders = [o for o in orders if o["type"] == "SELL"]

    random.shuffle(buy_orders)
    random.shuffle(sell_orders)
    match_count = min(len(buy_orders), len(sell_orders))
    print(f"Orders matched into trades: {match_count}")

    # 3. 执行匹配交易并模拟结果
    success_count = 0
    default_count = 0
    for i in range(match_count):
        buyer_order = buy_orders[i]
        seller_order = sell_orders[i]
        buyer = buyer_order["user"]
        seller = seller_order["user"]
        # 判断交易是否违约
        if random.random() < DEFAULT_PROB:
            # 交易违约
            default_count += 1
            # 随机决定违约方（买家或卖家之一违约）
            if random.choice([True, False]):
                defaulter = buyer
                non_defaulter = seller
            else:
                defaulter = seller
                non_defaulter = buyer
            # 调用链码记录违约交易结果
            invoke_cmd = (
                f'peer chaincode invoke '
                f'-o localhost:7050 '
                f'--ordererTLSHostnameOverride orderer.example.com '
                f'--tls --cafile /home/xingyu/Desktop/fabric-samples/test-network/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem '
                f'-C {CHANNEL_NAME} -n {CHAINCODE_NAME} '
                f'--peerAddresses localhost:7051 '
                f'--tlsRootCertFiles /home/xingyu/Desktop/fabric-samples/test-network/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt '
                f'--peerAddresses localhost:9051 '
                f'--tlsRootCertFiles /home/xingyu/Desktop/fabric-samples/test-network/organizations/peerOrganizations/org2.example.com/peers/peer0.org2.example.com/tls/ca.crt '
                f'-c \'{{"Args":["IssueToken","{buyer_order["order_id"]}","{seller_order["order_id"]}","DEFAULT","{defaulter}"]}}\' '
                '--waitForEvent'
            )
            result = subprocess.run(invoke_cmd, shell=True, capture_output=True, text=True)
            if result.returncode != 0:
                print(f"IssueToken DEFAULT failed for {buyer_order['order_id']} vs {seller_order['order_id']}: {result.stderr}")
            # 本地更新信誉值：违约方扣5分:contentReference[oaicite:12]{index=12}，守约方信誉不变
            reputation[defaulter] -= 5
            default_records.append({"round": rnd, "user": defaulter})
        else:
            # 交易成功
            success_count += 1
            invoke_cmd = (
                f'peer chaincode invoke '
                f'-o localhost:7050 '
                f'--ordererTLSHostnameOverride orderer.example.com '
                f'--tls --cafile /home/xingyu/Desktop/fabric-samples/test-network/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem '
                f'-C {CHANNEL_NAME} -n {CHAINCODE_NAME} '
                f'--peerAddresses localhost:7051 '
                f'--tlsRootCertFiles /home/xingyu/Desktop/fabric-samples/test-network/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt '
                f'--peerAddresses localhost:9051 '
                f'--tlsRootCertFiles /home/xingyu/Desktop/fabric-samples/test-network/organizations/peerOrganizations/org2.example.com/peers/peer0.org2.example.com/tls/ca.crt '
                f'-c \'{{"Args":["IssueToken","{buyer_order["order_id"]}","{seller_order["order_id"]}","SUCCESS",""]}}\' '
                '--waitForEvent'
            )
            result = subprocess.run(invoke_cmd, shell=True, capture_output=True, text=True)
            if result.returncode != 0:
                print(f"IssueToken SUCCESS failed for {buyer_order['order_id']} vs {seller_order['order_id']}: {result.stderr}")
            # 本地更新信誉值：买卖双方各加1分:contentReference[oaicite:13]{index=13}
            reputation[buyer] += 1
            reputation[seller] += 1

    # 4. 查询链码更新后的信誉值并记录
    # （假设链码维护每个用户的信誉值状态，可通过 QueryReputation(user) 获取）
    for user in users:
        query_cmd = (
            f'peer chaincode query -C {CHANNEL_NAME} -n {CHAINCODE_NAME} '
            f'-c \'{{"Args":["QueryReputation","{user}"]}}\''
        )
        result = subprocess.run(query_cmd, shell=True, capture_output=True, text=True)
        if result.returncode == 0:
            try:
                rep_value = int(result.stdout.strip())
            except ValueError:
                rep_value = result.stdout.strip()
        else:
            # 查询失败，使用本地维护的值替代
            rep_value = reputation[user]
        # 保存用户本轮后的信誉值
        rep_history.append({"round": rnd, "user": user, "reputation": rep_value})

    # 统计并记录本轮汇总数据
    round_stats.append({
        "round": rnd,
        "orders": len(orders),
        "matched": match_count,
        "success": success_count,
        "defaults": default_count,
        "excluded_users": USER_COUNT - len(orders)
    })
    print(f"Round {rnd} complete - Success: {success_count}, Defaults: {default_count}")

# 5. 将结果数据保存为 CSV 文件
df_rep = pd.DataFrame(rep_history)
df_rep.to_csv("reputation_history.csv", index=False)
df_defaults = pd.DataFrame(default_records)
df_defaults.to_csv("defaults_history.csv", index=False)
df_round = pd.DataFrame(round_stats)
df_round.to_csv("round_summary.csv", index=False)
print("\nSimulation completed. Data saved to CSV files.")

# 6. 绘制分析图表并保存
# 6.1 平均信誉变化折线图
avg_rep = df_rep.groupby("round")["reputation"].mean()
plt.figure(figsize=(6,4))
plt.plot(avg_rep.index, avg_rep.values, marker='o', color='blue')
plt.title("Average Reputation per Round")
plt.xlabel("Round")
plt.ylabel("Average Reputation")
plt.xticks(range(1, ROUND_COUNT+1))
plt.grid(True, linestyle='--', alpha=0.7)
plt.tight_layout()
plt.savefig("avg_reputation_per_round.png")
# 6.2 每轮成功率柱状图
plt.figure(figsize=(6,4))
success_rate = df_round["success"] / df_round["matched"] * 100
plt.bar(df_round["round"], success_rate, color='green', width=0.6)
plt.title("Trade Success Rate per Round")
plt.xlabel("Round")
plt.ylabel("Success Rate (%)")
plt.ylim(0, 105)
for x, y in zip(df_round["round"], success_rate):
    plt.text(x, y+1, f"{y:.1f}%", ha='center', va='bottom', fontsize=9)
plt.tight_layout()
plt.savefig("success_rate_per_round.png")
# 6.3 用户违约频次柱状图
plt.figure(figsize=(6,4))
# 统计每个用户的违约次数
default_counts = df_defaults["user"].value_counts()  # Series: user -> count
# 统计不同违约次数的人数分布
freq_dist = default_counts.value_counts().sort_index()
# 包含0次违约的用户数量（需手动计算，默认统计不包括0次的用户）
zero_defaults = USER_COUNT - len(default_counts)  # 从未违约的用户数
freq_dist = pd.Series({0: zero_defaults, **freq_dist.to_dict()}).sort_index()
plt.bar(freq_dist.index, freq_dist.values, color='orange', width=0.5)
plt.title("Distribution of Default Frequency")
plt.xlabel("Number of Defaults per User (out of 4 rounds)")
plt.ylabel("Number of Users")
for x, y in zip(freq_dist.index, freq_dist.values):
    plt.text(x, y+0.5, str(int(y)), ha='center', va='bottom')
plt.tight_layout()
plt.savefig("default_frequency_distribution.png")
print("Charts saved as PNG files.")
