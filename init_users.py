import os
import subprocess
import random
import json
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.backends import default_backend

# 配置Peer环境变量（以Org2身份）
os.environ['CORE_PEER_TLS_ENABLED'] = 'true'
os.environ['CORE_PEER_LOCALMSPID'] = 'Org2MSP'
os.environ['CORE_PEER_ADDRESS'] = 'localhost:9051'
os.environ['CORE_PEER_MSPCONFIGPATH'] = "/home/xingyu/Desktop/fabric-samples/test-network/organizations/peerOrganizations/org2.example.com/users/Admin@org2.example.com/msp"
os.environ['CORE_PEER_TLS_ROOTCERT_FILE'] = "/home/xingyu/Desktop/fabric-samples/test-network/organizations/peerOrganizations/org2.example.com/peers/peer0.org2.example.com/tls/ca.crt"

# 基础配置信息
CHANNEL_NAME = "mychannel"
CHAINCODE_NAME = "energyTrading"
ORDERER_ADDRESS = "localhost:7050"
ORDERER_CA = "/home/xingyu/Desktop/fabric-samples/test-network/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem"
PEER0_ORG1_ADDRESS = "localhost:7051"
PEER0_ORG1_TLS_CERT = "/home/xingyu/Desktop/fabric-samples/test-network/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt"
PEER0_ORG2_ADDRESS = "localhost:9051"
PEER0_ORG2_TLS_CERT = os.environ['CORE_PEER_TLS_ROOTCERT_FILE']

# 用户列表和初始数据
USER_COUNT = 100
users = [f"user{uid}" for uid in range(1, USER_COUNT + 1)]
reputation = {user: random.randint(40, 60) for user in users}
initial_balance = 1000

# 逐个用户生成独立的合法公钥，并调用链码
for user in users:
    rep = reputation[user]

    # 动态生成合法的 ECDSA 密钥对 (每个用户唯一的公钥)
    private_key = ec.generate_private_key(ec.SECP256R1(), default_backend())
    pem_public_key = private_key.public_key().public_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PublicFormat.SubjectPublicKeyInfo
    ).decode()

    # 自动进行 JSON 转义
    args = ["CreateParticipant", user, str(rep), str(initial_balance), pem_public_key]

    # 构建调用命令（推荐使用json.dumps，自动正确处理特殊字符）
    invoke_cmd = (
        f"peer chaincode invoke -o {ORDERER_ADDRESS} "
        f"--ordererTLSHostnameOverride orderer.example.com "
        f"--tls --cafile {ORDERER_CA} "
        f"-C {CHANNEL_NAME} -n {CHAINCODE_NAME} "
        f"--peerAddresses {PEER0_ORG1_ADDRESS} --tlsRootCertFiles {PEER0_ORG1_TLS_CERT} "
        f"--peerAddresses {PEER0_ORG2_ADDRESS} --tlsRootCertFiles {PEER0_ORG2_TLS_CERT} "
        f"-c '{json.dumps({'Args': args})}' "
        "--waitForEvent"
    )

    result = subprocess.run(invoke_cmd, shell=True, capture_output=True, text=True)
    if result.returncode != 0:
        print(f"[❌] Failed to create {user}: {result.stderr.strip()}")
    else:
        print(f"[✅] Registered {user} with reputation={rep}, balance={initial_balance}")
