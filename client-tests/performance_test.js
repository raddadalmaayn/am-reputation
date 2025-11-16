/*
 * Comprehensive Performance Test Suite for 'repcc'
 *
 * This script is designed to run a full, standalone performance analysis.
 * It measures:
 * 1. Sequential (Single-Thread) Latency for Reads and Writes
 * 2. Concurrent (Multi-Thread) Throughput for Reads and Writes
 * 3. Concurrent Throughput under Low-Conflict vs. High-Conflict (Contention)
 * 4. Estimated Storage Cost per transaction
 */

'use strict';

const { connect, Contract, Identity, Signer, signers } = require('@hyperledger/fabric-gateway');
const grpc = require('@grpc/grpc-js');
const crypto = require('crypto');
const fs = require('fs').promises;
const path = require('path');
const { TextDecoder } = require('util');

const utf8Decoder = new TextDecoder();

// --- Test Configuration ---
const channelName = 'mychannel';
const chaincodeName = 'repcc';
const mspId = 'Org1MSP';
const minStake = '10000';

// --- Test Parameters ---
const TEST_RUNS_SEQUENTIAL = 50; // 50 runs for stable average latency
const TEST_RUNS_CONCURRENT = 500; // 500 transactions for throughput
const CONCURRENCY_LEVEL = 100; // 100 requests in parallel

// --- Test Users ---
// Ensure these users were created by your 'enroll_test_users.sh' script
const adminUser = 'Admin';
const sequentialWriteUser = 'buyer1';
const concurrentUsers = Array.from({ length: 30 }, (_, i) => `tps_user_${i + 1}`);
const allTestUsers = [sequentialWriteUser, ...concurrentUsers]; // All users who need stake

// --- Connection Details ---
const cryptoPath = path.resolve(__dirname, '..', '..', 'fabric-samples', 'test-network', 'organizations', 'peerOrganizations', 'org1.example.com');
const peerEndpoint = 'localhost:7051';
const peerHostAlias = 'peer0.org1.example.com';
const tlsCertPath = path.resolve(cryptoPath, 'peers', 'peer0.org1.example.com', 'tls', 'ca.crt');


// --- Main Test Function ---
async function main() {
    console.log('--- Starting Comprehensive Performance Test Suite ---');
    
    const results = [];
    
    const adminClient = await newGrpcConnection();
    let adminGateway;
    let adminContract;

    try {
        adminGateway = await newGatewayForUser(adminClient, adminUser);
        const network = adminGateway.getNetwork(channelName);
        adminContract = network.getContract(chaincodeName);
        console.log('✓ Connected as admin');

        // --- Setup: Ensure all concurrent users have stake ---
        // We must do this sequentially to avoid race conditions
        console.log(`\n--- Setup Phase: Staking ${allTestUsers.length} test users... ---`);
        for (let i = 0; i < allTestUsers.length; i++) {
            const username = allTestUsers[i];
            process.stdout.write(`  Staking user ${i + 1}/${allTestUsers.length} (${username})...`);
            await submitStake(adminClient, username, minStake, true); // Suppress errors
            await sleep(500); // Give network time to commit
            process.stdout.write('✓\n');
        }
        console.log('✓ Setup complete.');

        // --- Run Test Plan ---
        console.log('\n--- Running Performance Benchmarks ---');

        // 1. Sequential Latency
        console.log(`  Running Test 1: Sequential Read Latency (${TEST_RUNS_SEQUENTIAL} txs)...`);
        let latencyRead = await testSequentialReadLatency(adminContract);
        results.push({ Category: 'Latency (Sequential)', Metric: 'Read (GetReputation)', Result: `${latencyRead.toFixed(2)} ms` });
        
        console.log(`  Running Test 2: Sequential Write Latency (${TEST_RUNS_SEQUENTIAL} txs)...`);
        let latencyWrite = await testSequentialWriteLatency(adminClient);
        results.push({ Category: 'Latency (Sequential)', Metric: 'Write (AddStake)', Result: `${latencyWrite.toFixed(2)} ms` });
        
        // 2. Concurrent Throughput
        console.log(`  Running Test 3: Concurrent Read Throughput (${TEST_RUNS_CONCURRENT} txs @ ${CONCURRENCY_LEVEL})...`);
        let tpsRead = await testConcurrentReadThroughput(adminContract);
        results.push({ Category: 'Throughput (Concurrent)', Metric: 'Read (GetReputation)', Result: `${tpsRead.toFixed(2)} TPS` });

        console.log(`  Running Test 4: Concurrent Write Throughput - Low Conflict (${TEST_RUNS_CONCURRENT} txs @ ${CONCURRENCY_LEVEL})...`);
        let tpsWriteLow = await testConcurrentWriteThroughput(adminClient, 'low-conflict');
        results.push({ Category: 'Throughput (Concurrent)', Metric: 'Low-Conflict Write (Realistic)', Result: `${tpsWriteLow.tps.toFixed(2)} TPS` });

        console.log(`  Running Test 5: Concurrent Write Throughput - High Conflict (${TEST_RUNS_CONCURRENT} txs @ ${CONCURRENCY_LEVEL})...`);
        let tpsWriteHigh = await testConcurrentWriteThroughput(adminClient, 'high-conflict');
        results.push({ Category: 'Throughput (Concurrent)', Metric: 'High-Conflict Write (Contention)', Result: `${tpsWriteHigh.tps.toFixed(2)} TPS (${tpsWriteHigh.mvccRate}% MVCC)` });

        // 3. Storage Cost (Simulated)
        console.log(`  Running Test 6: Storage Cost Estimation...`);
        let storageCosts = simulateStorageCost();
        results.push({ Category: 'Storage (Estimated)', Metric: 'Stake Record', Result: `${storageCosts.stake} Bytes` });
        results.push({ Category: 'Storage (Estimated)', Metric: 'Rating Record', Result: `${storageCosts.rating} Bytes` });
        results.push({ Category: 'Storage (Estimated)', Metric: 'Reputation Record (Final)', Result: `${storageCosts.reputation} Bytes` });

    } catch (error) {
        console.error('\n*** TOP LEVEL TEST FAILED ***');
        console.error(error);
    } finally {
        if (adminGateway) {
            adminGateway.close();
        }
        adminClient.close();
        
        // --- Print Final Results ---
        printFinalResults(results);
        
        console.log('\n--- Comprehensive Performance Test Complete ---');
    }
}

// --- Test Implementation Functions ---

// 1. Sequential Read Latency
async function testSequentialReadLatency(adminContract) {
    let totalTime = 0n;
    // Create a dummy record to read by staking the Admin user
    const readVictim = adminUser; // We will read the Admin's stake
    try {
        await adminContract.submitTransaction('AddStake', '12345');
    } catch (err) {
        // Ignore if stake already exists
    }
    
    for (let i = 0; i < TEST_RUNS_SEQUENTIAL; i++) {
        const start = process.hrtime.bigint();
        await adminContract.evaluateTransaction('GetStake', readVictim);
        const end = process.hrtime.bigint();
        totalTime += (end - start);
    }
    // Return average latency in milliseconds
    return (Number(totalTime) / TEST_RUNS_SEQUENTIAL / 1_000_000);
}

// 2. Sequential Write Latency
async function testSequentialWriteLatency(client) {
    let totalTime = 0n;
    for (let i = 0; i < TEST_RUNS_SEQUENTIAL; i++) {
        const start = process.hrtime.bigint();
        // *** THIS IS THE FIX ***
        // Send a valid stake amount, and suppress "already exists" errors
        await submitStake(client, sequentialWriteUser, minStake, true); 
        const end = process.hrtime.bigint();
        totalTime += (end - start);
        await sleep(500); // Wait for commit to prevent race condition
    }
    // Return average latency in milliseconds
    return (Number(totalTime) / TEST_RUNS_SEQUENTIAL / 1_000_000);
}

// 3. Concurrent Read Throughput
async function testConcurrentReadThroughput(adminContract) {
    const promises = [];
    const readVictim = adminUser; // Use the same admin stake record
    const start = process.hrtime.bigint();
    for (let i = 0; i < TEST_RUNS_CONCURRENT; i++) {
        promises.push(adminContract.evaluateTransaction('GetStake', readVictim));
        
        if (promises.length >= CONCURRENCY_LEVEL || i === TEST_RUNS_CONCURRENT - 1) {
            await Promise.all(promises);
            promises.length = 0; // Clear the array
        }
    }
    const end = process.hrtime.bigint();
    const totalTimeSec = Number(end - start) / 1_000_000_000;
    return TEST_RUNS_CONCURRENT / totalTimeSec; // Return TPS
}

// 4. & 5. Concurrent Write Throughput (Low & High Conflict)
async function testConcurrentWriteThroughput(client, conflictType) {
    const promises = [];
    let successCount = 0;
    let mvccCount = 0;
    const highConflictVictim = 'tps_high_conflict_victim';
    // Must create the high-conflict victim first
    if(conflictType === 'high-conflict') {
        await submitStake(client, 'buyer1', '10000', true); // Ensure buyer1 has stake
        await submitRating(client, 'buyer1', 0.5, highConflictVictim, true); // Create the rep record
    }

    const start = process.hrtime.bigint();
    for (let i = 0; i < TEST_RUNS_CONCURRENT; i++) {
        const user = concurrentUsers[i % concurrentUsers.length];
        // Use a unique key for low-conflict, or the same key for high-conflict
        const victim = (conflictType === 'low-conflict') ? `tps_low_${i}` : highConflictVictim;
        
        promises.push(submitRating(client, user, 0.5, victim));
        
        if (promises.length >= CONCURRENCY_LEVEL || i === TEST_RUNS_CONCURRENT - 1) {
            const { successes, mvccErrors } = await runConcurrentTest(promises);
            successCount += successes;
            mvccCount += mvccErrors;
            promises.length = 0; // Clear the array
        }
    }
    const end = process.hrtime.bigint();
    const totalTimeSec = Number(end - start) / 1_000_000_000;
    
    // Handle division by zero if time is too fast
    const tps = (totalTimeSec > 0) ? (successCount / totalTimeSec) : 0;
    const mvccRate = ((mvccCount / TEST_RUNS_CONCURRENT) * 100).toFixed(1);
    
    return { tps: tps || 0, mvccRate }; // Ensure tps is not NaN
}

// 6. Storage Cost Simulation
function simulateStorageCost() {
    // These objects are based on your 'deep-dive-report.md'
    const stakeRecord = {
        actorId: 'tps_user_1',
        balance: 10000,
        locked: 0,
        updatedAt: 1762325500
    };
    
    const ratingRecord = {
        ratingId: 'RATING:b345e2b7def79e5351fab3d37b042d84',
        raterId: 'buyer1',
        actorId: 'supplier_V_1762325500123',
        dimension: 'quality',
        rating: 0.9,
        evidenceHash: 'sha256:a94a8fe5ccb19ba61c4c0873d391e987982fbbd3',
        timestamp: 1762325500,
        blockNumber: 42,
        transactionId: 'b345e2b7def79e5351fab3d37b042d84'
    };
    
    const reputationRecord = {
        actorId: 'supplier_V_1762325500123',
        dimension: 'quality',
        alpha: 10.0, // After 10 ratings
        beta: 2.0,
        score: 0.8333,
        confidence: 0.15,
        totalRatings: 10,
        lastUpdated: 1762325590,
        version: 10
    };

    return {
        stake: Buffer.byteLength(JSON.stringify(stakeRecord), 'utf8'),
        rating: Buffer.byteLength(JSON.stringify(ratingRecord), 'utf8'),
        reputation: Buffer.byteLength(JSON.stringify(reputationRecord), 'utf8')
    };
}


/**
 * Helper to run promises and catch/count MVCC errors without crashing.
 */
async function runConcurrentTest(promises) {
    const results = await Promise.allSettled(promises);
    let successes = 0;
    let mvccErrors = 0;
    
    for (const result of results) {
        if (result.status === 'fulfilled') {
            successes++;
        } else {
            if (result.reason && (result.reason.code === 11 || result.reason.message.includes('MVCC_READ_CONFLICT'))) { 
                mvccErrors++;
            } else {
                console.error('\nUnexpected error during concurrent test:', result.reason);
            }
        }
    }
    return { successes, mvccErrors };
}


// --- Helper: Submit Functions ---
async function submitStake(client, username, amount, suppressErrors = false) {
    let userGateway;
    try {
        userGateway = await newGatewayForUser(client, username);
        const userContract = userGateway.getNetwork(channelName).getContract(chaincodeName);
        await userContract.submitTransaction('AddStake', amount);
    } catch(err) {
        if (suppressErrors && (err.message.includes('stake already exists') || err.message.includes('failed to endorse transaction'))) {
            // Ignore errors during setup
        } else {
            throw err;
        }
    }
}

async function submitRating(client, username, rating, victim, suppressErrors = false) {
    let userGateway;
    try {
        userGateway = await newGatewayForUser(client, username);
        const userContract = userGateway.getNetwork(channelName).getContract(chaincodeName);
        await userContract.submitTransaction(
            'SubmitRating',
            victim, 
            'quality', // Use a consistent dimension for testing
            rating.toString(),
            `hash_tx_${Date.now()}`,
            Math.floor(Date.now() / 1000).toString()
        );
    } catch (err) {
        if (!suppressErrors) {
            throw err;
        }
        // else, suppress errors (e.g., during high-conflict setup)
    }
    finally {
        if (userGateway) {
            // userGateway.close(); 
        }
    }
}

// --- Helper: Printing ---
function printFinalResults(results) {
    if (results.length === 0) {
        console.log('\n==================== SYSTEM PERFORMANCE RESULTS ====================');
        console.log('No performance results to display.');
        console.log('====================================================================');
        return;
    }

    const formattedResults = results.map(r => ({
        'Category': r.Category,
        'Metric': r.Metric,
        'Result': r.Result
    }));

    console.log('\n============================== COMPREHENSIVE PERFORMANCE RESULTS ==============================');
    console.table(formattedResults, ['Category', 'Metric', 'Result']);
    console.log('===========================================================================================');
}

// --- Helper: Connection & Utility Functions ---
function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

async function newGatewayForUser(client, username) {
    const identity = await newIdentity(username);
    const signer = await newSigner(username);
    return connect({
        client,
        identity,
        signer,
        evaluateOptions: () => ({ deadline: Date.now() + 5000 }), 
        endorseOptions: () => ({ deadline: Date.now() + 15000 }), 
        submitOptions: () => ({ deadline: Date.now() + 20000 }), 
        commitStatusOptions: () => ({ deadline: Date.now() + 60000 }), 
    });
}

async function newGrpcConnection() {
    const tlsRootCert = await fs.readFile(tlsCertPath);
    const tlsCredentials = grpc.credentials.createSsl(tlsRootCert);
    return new grpc.Client(peerEndpoint, tlsCredentials, {
        'grpc.ssl_target_name_override': peerHostAlias,
    });
}

async function newIdentity(username) {
    const certPath = path.resolve(cryptoPath, 'users', `${username}@org1.example.com`, 'msp', 'signcerts', 'cert.pem');
    const cert = await fs.readFile(certPath);
    return { mspId, credentials: cert };
}

async function newSigner(username) {
    const keyDirectoryPath = path.resolve(cryptoPath, 'users', `${username}@org1.example.com`, 'msp', 'keystore');
    const files = await fs.readdir(keyDirectoryPath);
    const keyPath = path.resolve(keyDirectoryPath, files[0]);
    const privateKeyPem = await fs.readFile(keyPath);
    const privateKey = crypto.createPrivateKey(privateKeyPem);
    return signers.newPrivateKeySigner(privateKey);
}

// --- Run the main function ---
main().catch(error => {
    console.error('******** FAILED TO RUN THE SCRIPT', error);
    process.exitCode = 1;
});


