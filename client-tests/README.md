# Client Tests for `repcc` Chaincode

This folder contains Node.js scripts to test the `repcc` (Reputation Contract) chaincode deployed on a Hyperledger Fabric network.

## 📦 Prerequisites
- Node.js v18 or newer
- npm (Node package manager)
- A running Fabric network with the `repcc` chaincode deployed on `mychannel`
- The Org1 admin identity available locally

## 📂 Files
- **test-repcc.js** → Runs functional tests against the `repcc` chaincode.

## ⚙️ Setup
1. Install dependencies:
   ```bash
   npm install @hyperledger/fabric-gateway @grpc/grpc-js
