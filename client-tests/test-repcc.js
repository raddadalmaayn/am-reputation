'use strict';

const { connect, signers } = require('@hyperledger/fabric-gateway');
const grpc = require('@grpc/grpc-js');
const crypto = require('crypto');
const fs = require('fs').promises;
const path = require('path');
const os = require('os');
const { TextDecoder } = require('util');

// ---------- Config ----------
const channelName = process.env.CHANNEL || 'mychannel';
const chaincodeName = process.env.CC || 'repcc';
const mspId = process.env.MSPID || 'Org1MSP';
const peerEndpoint = process.env.PEER_ENDPOINT || 'localhost:7051';
const peerHostAlias = process.env.PEER_HOST_ALIAS || 'peer0.org1.example.com';

const baseCrypto = process.env.CRYPTO_BASE ||
  path.resolve(os.homedir(), 'fabric-samples', 'test-network', 'organizations', 'peerOrganizations', 'org1.example.com');
const keyDirectoryPath = path.resolve(baseCrypto, 'users', 'Admin@org1.example.com', 'msp', 'keystore');
const certPath = path.resolve(baseCrypto, 'users', 'Admin@org1.example.com', 'msp', 'signcerts', 'cert.pem');
const tlsCertPath = path.resolve(baseCrypto, 'peers', 'peer0.org1.example.com', 'tls', 'ca.crt');

// ---------- Helpers ----------
const td = new TextDecoder();
const utf8 = (u8) => td.decode(u8);
const asJson = (u8) => JSON.parse(utf8(u8));
const sleep = (ms) => new Promise(r => setTimeout(r, ms));
const nowSec = () => Math.floor(Date.now() / 1000);
const randCid = (pfx='cid') => `${pfx}-${crypto.randomBytes(8).toString('hex')}`;

// Statistics accumulators
const stats = {
  latencies: [],
  throughputs: [],
  errors: 0,
  successful: 0,
  startTime: 0,
  endTime: 0
};

// ---------- Main Test Suite ----------
async function main() {
  console.log('╔════════════════════════════════════════════════════════════╗');
  console.log('║        REPUTATION SYSTEM KPI PERFORMANCE TEST              ║');
  console.log('╚════════════════════════════════════════════════════════════╝\n');
  
  console.log('Configuration:');
  console.log(`  Channel: ${channelName}`);
  console.log(`  Chaincode: ${chaincodeName}`);
  console.log(`  MSP: ${mspId}`);
  console.log(`  Peer: ${peerEndpoint}\n`);

  const client = await newGrpcConnection();
  const gateway = connect({
    client,
    identity: await newIdentity(),
    signer: await newSigner(),
    endorseOptions: () => ({ deadline: Date.now() + 15000 }),
    submitOptions: () => ({ deadline: Date.now() + 30000 }),
    commitStatusOptions: () => ({ deadline: Date.now() + 30000 }),
  });

  try {
    const contract = gateway.getNetwork(channelName).getContract(chaincodeName);

    // Run all KPI tests
    await testSystemPerformance(contract);
    await testStakingMechanism(contract);
    await testDisputeFlow(contract);
    await testBatchOperations(contract);
    await testScalability(contract);
    await testDataRetrieval(contract);
    
    // Generate final report
    generateReport();

  } catch (e) {
    console.error('\n❌ Fatal error:', e?.message || e);
    if (e.stack) console.error(e.stack);
    process.exitCode = 1;
  } finally {
    gateway.close();
    client.close();
  }
}

// ============================================================================
// KPI 1: TRANSACTION LATENCY & THROUGHPUT
// ============================================================================
async function testSystemPerformance(contract) {
  console.log('\n' + '='.repeat(60));
  console.log('TEST 1: Transaction Latency & Throughput');
  console.log('='.repeat(60));

  const actors = ['supplier_A', 'supplier_B', 'manufacturer_C'];
  const dimensions = ['quality', 'delivery', 'compliance'];
  const numTransactions = 100;

  stats.startTime = Date.now();
  
  console.log(`\nSubmitting ${numTransactions} rating transactions...\n`);
  
  for (let i = 0; i < numTransactions; i++) {
    const actor = actors[i % actors.length];
    const dimension = dimensions[i % dimensions.length];
    const value = (Math.random() * 0.4 + 0.6).toFixed(2); // Random 0.6-1.0
    const cid = randCid(`perf_${i}`);
    const ts = String(nowSec() + i);
    
    const txStart = Date.now();
    
    try {
      await contract.submitTransaction('SubmitRating', actor, dimension, value, cid, ts);
      const txEnd = Date.now();
      const latency = txEnd - txStart;
      stats.latencies.push(latency);
      stats.successful++;
      
      if ((i + 1) % 10 === 0) {
        process.stdout.write(`Progress: ${i + 1}/${numTransactions} (Avg latency: ${avgLatency().toFixed(2)}ms)\r`);
      }
    } catch (e) {
      stats.errors++;
      console.error(`\nError on transaction ${i}:`, e.message);
    }
    
    // Small delay to avoid overwhelming the network
    if (i < numTransactions - 1) await sleep(50);
  }
  
  stats.endTime = Date.now();
  const totalTime = (stats.endTime - stats.startTime) / 1000; // seconds
  const tps = stats.successful / totalTime;
  
  console.log('\n\nPerformance Results:');
  console.log(`  Total transactions: ${numTransactions}`);
  console.log(`  Successful: ${stats.successful}`);
  console.log(`  Failed: ${stats.errors}`);
  console.log(`  Total time: ${totalTime.toFixed(2)}s`);
  console.log(`  Throughput: ${tps.toFixed(2)} TPS`);
  console.log(`  Avg latency: ${avgLatency().toFixed(2)}ms`);
  console.log(`  Median latency: ${medianLatency().toFixed(2)}ms`);
  console.log(`  95th percentile: ${percentileLatency(95).toFixed(2)}ms`);
  console.log(`  99th percentile: ${percentileLatency(99).toFixed(2)}ms`);
}

// ============================================================================
// KPI 2: STORAGE OVERHEAD
// ============================================================================
async function testDataRetrieval(contract) {
  console.log('\n' + '='.repeat(60));
  console.log('TEST 2: Storage Overhead & Query Performance');
  console.log('='.repeat(60));

  const testActors = ['supplier_A', 'supplier_B', 'manufacturer_C'];
  
  console.log('\nQuerying reputation scores...\n');
  
  for (const actor of testActors) {
    const queryStart = Date.now();
    const rep = await contract.evaluateTransaction('GetReputation', actor, 'quality');
    const queryEnd = Date.now();
    const queryLatency = queryEnd - queryStart;
    
    const repData = asJson(rep);
    const payloadSize = Buffer.byteLength(JSON.stringify(repData));
    
    console.log(`Actor: ${actor}`);
    console.log(`  Query latency: ${queryLatency}ms`);
    console.log(`  Payload size: ${payloadSize} bytes`);
    console.log(`  Score: ${repData.score?.toFixed(4) || 'N/A'}`);
    console.log(`  Confidence: ${repData.confidence?.toFixed(4) || 'N/A'}`);
    console.log(`  Total events: ${repData.totalEvents || 0}\n`);
  }
  
  // Query system metrics
  console.log('Querying system-wide metrics...');
  const metricsStart = Date.now();
  const metrics = await contract.evaluateTransaction('GetSystemMetrics');
  const metricsEnd = Date.now();
  const metricsData = asJson(metrics);
  
  console.log(`  Query latency: ${metricsEnd - metricsStart}ms`);
  console.log(`  Total ratings: ${metricsData.totalRatings || 0}`);
  console.log(`  Total disputes: ${metricsData.totalDisputes || 0}`);
  console.log(`  Stake slashed: $${metricsData.totalStakeSlashed || 0}\n`);
}

// ============================================================================
// KPI 3: STAKING MECHANISM
// ============================================================================
async function testStakingMechanism(contract) {
  console.log('\n' + '='.repeat(60));
  console.log('TEST 3: Staking Mechanism');
  console.log('='.repeat(60));

  console.log('\nAdding stake...');
  const stakeAmount = '5000';
  
  const stakeStart = Date.now();
  await contract.submitTransaction('AddStake', stakeAmount);
  const stakeEnd = Date.now();
  
  console.log(`  Stake added: $${stakeAmount}`);
  console.log(`  Transaction latency: ${stakeEnd - stakeStart}ms`);
  
  await sleep(1000);
  
  // Verify stake was added by submitting a rating (requires stake)
  console.log('\nVerifying stake requirement enforcement...');
  try {
    const result = await contract.submitTransaction(
      'SubmitRating',
      'test_actor',
      'quality',
      '0.9',
      randCid('stake_test'),
      String(nowSec())
    );
    console.log('  ✓ Rating submitted successfully (stake verified)');
  } catch (e) {
    console.log('  ✗ Rating rejected:', e.message);
  }
}

// ============================================================================
// KPI 4: DISPUTE RESOLUTION
// ============================================================================
async function testDisputeFlow(contract) {
  console.log('\n' + '='.repeat(60));
  console.log('TEST 4: Dispute Resolution Flow');
  console.log('='.repeat(60));

  // First, create a rating to dispute
  const disputeActor = 'disputed_supplier';
  const disputeCid = randCid('dispute_target');
  const disputeTs = String(nowSec());
  
  console.log('\nCreating a rating to dispute...');
  const ratingId = await contract.submitTransaction(
    'SubmitRating',
    disputeActor,
    'quality',
    '0.3', // Low rating
    disputeCid,
    disputeTs
  );
  
  console.log(`  Rating created with low score (0.3)`);
  await sleep(1000);
  
  // Construct rating ID (format from chaincode: RAT-{hash})
  const ratingIdStr = `RAT-${crypto.createHash('sha256')
    .update(`${disputeActor}:quality:${disputeTs}:${disputeCid}`)
    .digest('hex')
    .slice(0, 16)}`;
  
  console.log('\nInitiating dispute...');
  const disputeStart = Date.now();
  
  try {
    const disputeId = await contract.submitTransaction(
      'InitiateDispute',
      ratingIdStr,
      'Rating appears inflated or biased',
      'QmEvidenceHash123'
    );
    const disputeEnd = Date.now();
    
    console.log(`  Dispute initiated: ${utf8(disputeId)}`);
    console.log(`  Transaction latency: ${disputeEnd - disputeStart}ms`);
    
    await sleep(1000);
    
    // Resolve dispute (would normally be done by arbitrator)
    console.log('\nResolving dispute...');
    const resolveStart = Date.now();
    await contract.submitTransaction(
      'ResolveDispute',
      utf8(disputeId),
      'overturned' // or 'upheld'
    );
    const resolveEnd = Date.now();
    
    console.log(`  Dispute resolved`);
    console.log(`  Resolution latency: ${resolveEnd - resolveStart}ms`);
    
  } catch (e) {
    console.log(`  Error in dispute flow: ${e.message}`);
  }
}

// ============================================================================
// KPI 5: BATCH OPERATIONS
// ============================================================================
async function testBatchOperations(contract) {
  console.log('\n' + '='.repeat(60));
  console.log('TEST 5: Batch Query Performance');
  console.log('='.repeat(60));

  const actors = ['supplier_A', 'supplier_B', 'manufacturer_C', 'supplier_D', 'supplier_E'];
  
  console.log(`\nBatch querying ${actors.length} actors...\n`);
  
  const batchStart = Date.now();
  const batchQueries = actors.map(actor => 
    contract.evaluateTransaction('GetReputation', actor, 'quality')
  );
  
  const results = await Promise.all(batchQueries);
  const batchEnd = Date.now();
  
  const batchTime = batchEnd - batchStart;
  const avgPerQuery = batchTime / actors.length;
  
  console.log(`Batch query results:`);
  console.log(`  Total time: ${batchTime}ms`);
  console.log(`  Queries: ${actors.length}`);
  console.log(`  Avg per query: ${avgPerQuery.toFixed(2)}ms`);
  console.log(`  Effective QPS: ${(1000 / avgPerQuery).toFixed(2)}\n`);
  
  results.forEach((result, idx) => {
    const data = asJson(result);
    console.log(`  ${actors[idx]}: score=${data.score?.toFixed(4) || 'N/A'}, events=${data.totalEvents || 0}`);
  });
}

// ============================================================================
// KPI 6: SCALABILITY TEST
// ============================================================================
async function testScalability(contract) {
  console.log('\n' + '='.repeat(60));
  console.log('TEST 6: Scalability Under Load');
  console.log('='.repeat(60));

  const loads = [10, 50, 100];
  
  for (const load of loads) {
    console.log(`\nTesting with ${load} concurrent transactions...`);
    
    const concurrentStart = Date.now();
    const promises = [];
    
    for (let i = 0; i < load; i++) {
      promises.push(
        contract.submitTransaction(
          'SubmitRating',
          `actor_${i % 10}`,
          'quality',
          String(0.7 + Math.random() * 0.3),
          randCid(`scale_${load}_${i}`),
          String(nowSec() + i)
        ).catch(e => ({ error: e.message }))
      );
    }
    
    const results = await Promise.all(promises);
    const concurrentEnd = Date.now();
    
    const errors = results.filter(r => r?.error).length;
    const successful = results.length - errors;
    const totalTime = (concurrentEnd - concurrentStart) / 1000;
    const tps = successful / totalTime;
    
    console.log(`  Results:`);
    console.log(`    Successful: ${successful}/${load}`);
    console.log(`    Failed: ${errors}`);
    console.log(`    Time: ${totalTime.toFixed(2)}s`);
    console.log(`    TPS: ${tps.toFixed(2)}`);
  }
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

function avgLatency() {
  if (stats.latencies.length === 0) return 0;
  return stats.latencies.reduce((a, b) => a + b, 0) / stats.latencies.length;
}

function medianLatency() {
  if (stats.latencies.length === 0) return 0;
  const sorted = [...stats.latencies].sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 === 0 ? (sorted[mid - 1] + sorted[mid]) / 2 : sorted[mid];
}

function percentileLatency(p) {
  if (stats.latencies.length === 0) return 0;
  const sorted = [...stats.latencies].sort((a, b) => a - b);
  const idx = Math.ceil((p / 100) * sorted.length) - 1;
  return sorted[Math.max(0, idx)];
}

function generateReport() {
  console.log('\n' + '═'.repeat(60));
  console.log('FINAL REPORT - KEY PERFORMANCE INDICATORS');
  console.log('═'.repeat(60));
  
  console.log('\n1. TRANSACTION PERFORMANCE:');
  console.log(`   • Average Latency: ${avgLatency().toFixed(2)}ms`);
  console.log(`   • Median Latency: ${medianLatency().toFixed(2)}ms`);
  console.log(`   • 95th Percentile: ${percentileLatency(95).toFixed(2)}ms`);
  console.log(`   • 99th Percentile: ${percentileLatency(99).toFixed(2)}ms`);
  console.log(`   • Throughput: ${(stats.successful / ((stats.endTime - stats.startTime) / 1000)).toFixed(2)} TPS`);
  
  console.log('\n2. RELIABILITY:');
  console.log(`   • Total Transactions: ${stats.successful + stats.errors}`);
  console.log(`   • Successful: ${stats.successful}`);
  console.log(`   • Failed: ${stats.errors}`);
  console.log(`   • Success Rate: ${((stats.successful / (stats.successful + stats.errors)) * 100).toFixed(2)}%`);
  
  console.log('\n3. NEXT STEPS:');
  console.log('   • Export data to CSV for analysis');
  console.log('   • Generate graphs for paper');
  console.log('   • Compare with baseline measurements');
  console.log('   • Run MARL simulations with these parameters\n');
}

// ============================================================================
// GATEWAY SETUP
// ============================================================================

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

// ============================================================================
// RUN
// ============================================================================

main();
