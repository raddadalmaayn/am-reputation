'use strict';

const { connect, signers } = require('@hyperledger/fabric-gateway');
const grpc = require('@grpc/grpc-js');
const crypto = require('crypto');
const fs = require('fs').promises;
const path = require('path');
const os = require('os');
const { TextDecoder } = require('util');

// ---------- Config (change if needed) ----------
const channelName = process.env.CHANNEL || 'mychannel';
const chaincodeName = process.env.CC || 'repcc';
const mspId = process.env.MSPID || 'Org1MSP';

const peerEndpoint = process.env.PEER_ENDPOINT || 'localhost:7051';
const peerHostAlias = process.env.PEER_HOST_ALIAS || 'peer0.org1.example.com';

// Crypto material (defaults to ~/fabric-samples/test-network for Org1 Admin)
const baseCrypto = process.env.CRYPTO_BASE ||
  path.resolve(os.homedir(), 'fabric-samples', 'test-network', 'organizations', 'peerOrganizations', 'org1.example.com');
const keyDirectoryPath = path.resolve(baseCrypto, 'users', 'Admin@org1.example.com', 'msp', 'keystore');
const certPath = path.resolve(baseCrypto, 'users', 'Admin@org1.example.com', 'msp', 'signcerts', 'cert.pem');
const tlsCertPath = path.resolve(baseCrypto, 'peers', 'peer0.org1.example.com', 'tls', 'ca.crt');

// Defaults for demo inputs (override via CLI: node test-repcc.js actorX quality 1)
const [,, ACTOR = 'actorA', DIM = 'quality', VAL = '1'] = process.argv;

// ---------- Helpers ----------
const td = new TextDecoder();
const utf8 = (u8) => td.decode(u8);                // Uint8Array -> string
const asJson = (u8) => JSON.parse(utf8(u8));       // Uint8Array -> object
const sleep = (ms) => new Promise(r => setTimeout(r, ms));
const nowSec = () => Math.floor(Date.now() / 1000);
const randCid = (pfx='cid') => `${pfx}-${crypto.randomBytes(8).toString('hex')}`;

// ---------- Main ----------
async function main() {
  console.log('== repcc test ==');
  console.log({ channelName, chaincodeName, mspId, peerEndpoint, peerHostAlias });
  console.log('Crypto base:', baseCrypto);

  const client = await newGrpcConnection();
  const gateway = connect({
    client,
    identity: await newIdentity(),
    signer: await newSigner(),
    // generous timeouts for local dev
    endorseOptions: () => ({ deadline: Date.now() + 15000 }),
    submitOptions: () => ({ deadline: Date.now() + 30000 }),
    commitStatusOptions: () => ({ deadline: Date.now() + 30000 }),
  });

  try {
    const contract = gateway.getNetwork(channelName).getContract(chaincodeName);

    // 1) Submit success rating
    const ts1 = String(nowSec());
    const cid1 = randCid('cid');
    console.log(`\nSubmitting rating: actor=${ACTOR}, dim=${DIM}, value=${VAL}, cid=${cid1}, ts=${ts1}`);
    const res1 = await contract.submitTransaction('SubmitRating', ACTOR, DIM, String(VAL), cid1, ts1);
    console.log('SubmitRating result:', Number(utf8(res1))); // 1 means applied

    // Small pause
    await sleep(500);

    // Query reputation
    const rep1 = await contract.evaluateTransaction('GetReputation', ACTOR, DIM);
    const rep1Obj = asJson(rep1);
    console.log('Reputation after first submit:', rep1Obj);
    console.log('Smoothed score:', smoothed(rep1Obj).toFixed(4));

    // 2) Duplicate same rating (should be 0)
    const dup = await contract.submitTransaction('SubmitRating', ACTOR, DIM, String(VAL), cid1, ts1);
    console.log('Duplicate SubmitRating result (should be 0):', Number(utf8(dup)));
    const rep2 = await contract.evaluateTransaction('GetReputation', ACTOR, DIM);
    console.log('Reputation after duplicate:', asJson(rep2));

    // 3) Submit a failure (value <= 0)
    const ts2 = String(nowSec());
    const cid2 = randCid('cid');
    const resFail = await contract.submitTransaction('SubmitRating', ACTOR, DIM, '0', cid2, ts2);
    console.log('SubmitRating failure result:', Number(utf8(resFail))); // 1 means applied
    const rep3 = asJson(await contract.evaluateTransaction('GetReputation', ACTOR, DIM));
    console.log('Reputation after failure submit:', rep3);
    console.log('Smoothed score:', smoothed(rep3).toFixed(4));

    console.log('\n✅ Test flow complete.');

  } catch (e) {
    console.error('❌ Test error:', e?.message || e);
    process.exitCode = 1;
  } finally {
    gateway.close();
    client.close();
  }
}

function smoothed(r) {
  // Laplace smoothing: (alpha+1) / (alpha+beta+2)
  const a = Number(r.alpha || 0);
  const b = Number(r.beta || 0);
  return (a + 1) / (a + b + 2);
}

// ---------- Gateway plumbing ----------
async function newGrpcConnection() {
  const tlsRootCert = await fs.readFile(tlsCertPath);
  const tlsCredentials = grpc.credentials.createSsl(tlsRootCert);
  return new grpc.Client(peerEndpoint, tlsCredentials, {
    'grpc.ssl_target_name_override': peerHostAlias,
    'grpc.max_send_message_length': -1,
    'grpc.max_receive_message_length': -1,
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
