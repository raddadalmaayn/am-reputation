#!/bin/bash
#
# This master script runs the *entire* process from a clean slate.
# 1. Wipes the old network
# 2. Starts a new network + deploys chaincode
# 3. Enrolls all 42 test users
# 4. Runs the comprehensive performance test script

set -e

# --- Paths ---
TEST_NETWORK_PATH="/home/raddad/fabric-samples/test-network"
CHAINCODE_PATH="~/am-reputation/chaincode"
CLIENT_TEST_PATH="/home/raddad/am-reputation/client-tests"

# --- 1. Shut Down and Restart Network ---
echo "--- [1/4] Shutting down and restarting Fabric network... ---"
cd ${TEST_NETWORK_PATH}
./network.sh down
./network.sh up createChannel -c mychannel -ca
echo "--- Network is UP ---"

# --- 2. Deploy Chaincode ---
echo "--- [2/4] Deploying 'repcc' chaincode... ---"
./network.sh deployCC -ccn repcc -ccp ${CHAINCODE_PATH} -ccl go -c mychannel
echo "--- Chaincode deployed ---"

# --- 3. Enroll All Test Users ---
echo "--- [3/4] Enrolling all 42 test users... ---"
cd ${CLIENT_TEST_PATH}
# Ensure the enrollment script is executable
chmod +x enroll_test_users.sh
./enroll_test_users.sh
echo "--- All users enrolled ---"

# --- 4. Run Comprehensive Performance Test ---
echo "--- [4/4] Running comprehensive performance test... ---"
# This is the 'performance_test.js' script from our previous conversation
node performance_test.js

echo "--- [COMPLETE] Full test suite finished. ---"

