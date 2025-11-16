#!/bin/bash
#
# This script enrolls all users needed for the comprehensive test:
# - Admin: For resolving disputes
# - 10 Buyers: For honest ratings
# - 1 Attacker: For the economic test
# - 1 Victim: For the economic test
# - 30+ Throughput Users: For the performance test

# --- Configuration ---
TEST_NETWORK_PATH="/home/raddad/fabric-samples/test-network"
CA_ADMIN_USER="admin"
CA_ADMIN_PASS="adminpw"
CA_URL="localhost:7054" # Org1 CA
ORG_AFFILIATION="org1.department1"
# ---------------------

# Set the environment variable for the CA client
export FABRIC_CA_CLIENT_HOME=${TEST_NETWORK_PATH}/organizations/peerOrganizations/org1.example.com/
CA_TLS_CERT_PATH="${TEST_NETWORK_PATH}/organizations/fabric-ca/org1/tls-cert.pem"

echo "--- Enrolling Test Users ---"

# Function to enroll a single user
# Usage: enroll_user <USERNAME> <SECRET> [AFFILIATION] [ATTRIBUTES (optional)]
enroll_user() {
    local USERNAME=$1
    local USER_PASS=$2
    local AFFILIATION=$3
    local ATTRS=$4

    echo "Registering user: ${USERNAME}..."
    
    # Build register command
    local REG_CMD="fabric-ca-client register --id.name ${USERNAME} --id.secret ${USER_PASS} --id.type client --id.affiliation ${AFFILIATION} -u https://${CA_ADMIN_USER}:${CA_ADMIN_PASS}@${CA_URL} --tls.certfiles ${CA_TLS_CERT_PATH}"
    
    # Add optional attributes if provided
    if [ ! -z "$ATTRS" ]; then
        REG_CMD+=" --id.attrs '$ATTRS'"
    fi
    
    eval $REG_CMD
    
    echo "Enrolling user: ${USERNAME}..."
    fabric-ca-client enroll -u https://${USERNAME}:${USER_PASS}@${CA_URL} --caname ca-org1 -M "${FABRIC_CA_CLIENT_HOME}/users/${USERNAME}@org1.example.com/msp" --tls.certfiles ${CA_TLS_CERT_PATH}
    
    echo "User ${USERNAME} created successfully."
}

# --- Enroll Users ---

# 1. Enroll the Admin (needed for resolving disputes)
# The default 'Admin' user already has the right perms, so we just enroll them.
echo "Enrolling Admin..."
fabric-ca-client enroll -u https://admin:adminpw@${CA_URL} --caname ca-org1 -M "${FABRIC_CA_CLIENT_HOME}/users/Admin@org1.example.com/msp" --tls.certfiles ${CA_TLS_CERT_PATH}
echo "Admin enrolled."

# 2. Enroll 10 'buyers' for honest ratings
for i in $(seq 1 10); do
    enroll_user "buyer${i}" "buyer${i}pw" "${ORG_AFFILIATION}"
done

# 3. Enroll 1 'attacker'
enroll_user "attacker_1" "attacker1pw" "${ORG_AFFILIATION}"

# 4. Enroll 1 'victim_supplier'
enroll_user "victim_supplier" "victim1pw" "${ORG_AFFILIATION}"

# 5. Enroll 30 'throughput' users (for performance test)
for i in $(seq 1 30); do
    enroll_user "tps_user_${i}" "tps_user_pw_${i}" "${ORG_AFFILIATION}"
done

echo "--- All Test Users Enrolled Successfully ---"

