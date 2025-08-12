'use strict';

const { connect, signers } = require('@hyperledger/fabric-gateway');
const grpc = require('@grpc/grpc-js');
const crypto = require('crypto');
const fs = require('fs').promises;
const path = require('path');

const channelName = 'mychannel';
const chaincodeName = 'repcc';
const mspId = 'Org1MSP';

const cryptoPath = path.resolve(__dirname, '..', 'fabric-samples', 'test-network', 'organizations', 'peerOrganizations', 'org1.example.com');
const keyDirectoryPath = path.resolve(cryptoPath, 'users', 'Admin@org1.example.com', 'msp', 'keystore');
const certPath = path.resolve(cryptoPath, 'users', 'Admin@org1.example.com', 'msp', 'signcerts', 'cert.pem');
const tlsCertPath = path.resolve(cryptoPath, 'peers', 'peer0.org1.example.com', 'tls', 'ca.crt');
const peerEndpoint = 'localhost:7051';
const peerHostAlias = 'peer0.org1.example.com';

async function main() {
    const client = await newGrpcConnection();
    const gateway = connect({
        client,
        identity: await newIdentity(),
        signer: await newSigner()
    });

    try {
        const network = gateway.getNetwork(channelName);
        const contract = network.getContract(chaincodeName);

        console.log('Submitting rating...');
        const ts = Math.floor(Date.now() / 1000).toString();
        const result = await contract.submitTransaction('SubmitRating', 'actor1', 'quality', '1', crypto.randomUUID(), ts);
        console.log('SubmitRating result:', result.toString());

        console.log('Fetching reputation...');
        const rep = await contract.evaluateTransaction('GetReputation', 'actor1', 'quality');
        console.log('Reputation record:', rep.toString());

    } catch (err) {
        console.error('Error:', err);
    } finally {
        gateway.close();
        client.close();
    }
}

async function newGrpcConnection() {
    const tlsRootCert = await fs.readFile(tlsCertPath);
    const tlsCredentials = grpc.credentials.createSsl(tlsRootCert);
    return new grpc.Client(peerEndpoint, tlsCredentials, {
        'grpc.ssl_target_name_override': peerHostAlias
    });
}

async function newIdentity() {
    const credentials = await fs.readFile(certPath);
    return { mspId, credentials };
}

async function newSigner() {
    const files = await fs.readdir(keyDirectoryPath);
    const keyPath = path.resolve(keyDirectoryPath, files[0]);
    const privateKeyPem = await fs.readFile(keyPath);
    const privateKey = crypto.createPrivateKey(privateKeyPem);
    return signers.newPrivateKeySigner(privateKey);
}

main();
