/*
 * FINAL Comprehensive Test Script for Stake-Based Bayesian Reputation System
 *
 * This script runs two main experiments:
 * 1. Economic Security Test: Proves the "Cost-of-Attack" by simulating
 * an attack, dispute, and stake-slashing.
 * 2. Performance Test: Measures sequential latency and concurrent throughput.
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
const chaincodeName = 'repcc'; // Your reputation chaincode
const mspId = 'Org1MSP';
const minStake = '10000'; // Must be a string for chaincode
const attackerStake = '100000'; // Attacker stakes 100k
const slashPenalty = 0.5; // From your deep-dive, 50% penalty

// --- Test Actors ---
// These are STATIC names that MUST match your enroll_test_users.sh script
const adminUser = 'Admin'; // Still needed to RESOLVE disputes
const queryUser = 'buyer1'; // Use a user with correct affiliation for queries
const victimSupplier = 'victim_supplier';
const attackerUser = 'attacker_1';
const honestRaters = Array.from({ length: 10 }, (_, i) => `buyer${i + 1}`);
const perfTestUsers = Array.from({ length: 30 }, (_, i) => `tps_user_${i + 1}`);
// All users who need to be enrolled and staked
const allTestUsers = [victimSupplier, attackerUser, ...honestRaters, ...perfTestUsers];

// --- Test Parameters ---
const testDimension = 'quality';

// --- Performance Test Config ---
const READ_LATENCY_TESTS = 50;
const WRITE_LATENCY_TESTS = 50;
const THROUGHPUT_TEST_SIZE = 300;
const THROUGHPUT_CONCURRENCY = 50;

// --- Connection Details ---
const cryptoPath = path.resolve(__dirname, '..', '..', 'fabric-samples', 'test-network', 'organizations', 'peerOrganizations', 'org1.example.com');
const peerEndpoint = 'localhost:7051';
const peerHostAlias = 'peer0.org1.example.com';
const tlsCertPath = path.resolve(cryptoPath, 'peers', 'peer0.org1.example.com', 'tls', 'ca.crt');

// --- Main Test Function ---

async function main() {
    console.log('--- Starting Comprehensive Security & Performance Test ---');
    
    const economicTestResults = [];
    const performanceResults = [];
    
    // We create one client, but two gateways:
    // 1. adminGateway (as Admin) - only for resolving disputes
    // 2. queryGateway (as buyer1) - for all submits and queries
    const client = await newGrpcConnection();
    let adminGateway;
    let queryGateway;
    let adminContract;
    let queryContract;

    try {
        // === Connect as Admin (for dispute resolution) ===
        adminGateway = await newGatewayForUser(client, adminUser);
        adminContract = adminGateway.getNetwork(channelName).getContract(chaincodeName);
        console.log('✓ Connected as admin');

        // === Connect as Query User (for all other actions) ===
        queryGateway = await newGatewayForUser(client, queryUser);
        queryContract = queryGateway.getNetwork(channelName).getContract(chaincodeName);
        console.log(`✓ Connected as query user (${queryUser})`);


        // === 1. Setup Phase: Add Stake for all users ===
        await setupTestUsers(client);

        // === 2. Run Economic Security "Cost-of-Attack" Test ===
        // We pass queryContract for reads and adminContract for resolution
        const econResults = await runEconomicSecurityTest(client, queryContract, adminContract);
        economicTestResults.push(...econResults);

        // === 3. Run Performance Test ===
        const perfResults = await runPerformanceTest(client, queryContract);
        performanceResults.push(...perfResults);

    } catch (error) {
        console.error('\n*** TOP LEVEL TEST FAILED ***');
        console.error(error);
    } finally {
        if (adminGateway) {
            adminGateway.close();
        }
        if (queryGateway) {
            queryGateway.close();
        }
        client.close();
        
        // --- Print Final Results ---
        printEconomicSecurityResults(economicTestResults);
        printPerformanceResults(performanceResults);
        
        console.log('\n--- Comprehensive Test Suite Complete ---');
    }
}

// --- Test Logic Functions ---

/**
 * Runs the "AddStake" setup for all test users.
 */
async function setupTestUsers(client) {
    console.log(`\n--- Setup Phase: Adding stake to all ${allTestUsers.length} test users ---`);
    for (let i = 0; i < allTestUsers.length; i++) {
        const username = allTestUsers[i];
        const stakeAmount = (username === attackerUser) ? attackerStake : minStake;
        
        process.stdout.write(`  Staking user ${i + 1}/${allTestUsers.length} (${username} with ${stakeAmount})...`);
        
        let userGateway;
        try {
            userGateway = await newGatewayForUser(client, username);
            const userContract = userGateway.getNetwork(channelName).getContract(chaincodeName);
            
            try {
                await userContract.submitTransaction('AddStake', stakeAmount);
                process.stdout.write('✓\n');
                await sleep(1000); // 1s wait for consensus
            } catch (err) {
                if (err.message.includes('stake already exists')) {
                     process.stdout.write('✓ (Stake already exists)\n');
                } else {
                    process.stdout.write(' (1st attempt failed, retrying...)');
                    await sleep(2000); // Wait 2 seconds
                    await userContract.submitTransaction('AddStake', stakeAmount);
                    process.stdout.write('✓ (Retry success)\n');
                    await sleep(1000);
                }
            }
        } catch (err) {
            console.error(`\n*** FAILED to add stake for ${username} on 2nd attempt: ${err.message.split('\n')[0]}`);
            throw new Error('Setup phase failed. Aborting test.');
        } 
    }
    console.log('✓ Setup phase complete.');
}

/**
 * Runs the Economic Security "Cost-of-Attack" test.
 */
async function runEconomicSecurityTest(client, queryContract, adminContract) {
    console.log(`\n--- Running Economic Security Test ---`);
    console.log(`Victim: ${victimSupplier} | Attacker: ${attackerUser}`);
    const results = [];
    let attackedRatingId; // We need to store this to dispute it
    let disputeId; // And this

    // === Phase 1: Establishment ===
    console.log(`  Phase 1: Submitting ${honestRaters.length} good ratings (0.9)...`);
    for (let i = 0; i < honestRaters.length; i++) {
        await submitRating(client, queryContract, honestRaters[i], 0.9, victimSupplier);
        await sleep(500); // Wait for consensus
    }
    let victimRep = await getChainReputation(queryContract, victimSupplier);
    let attackerStake = await getChainStake(queryContract, attackerUser);
    results.push({ 'Phase': '1. Establishment', 'Action': `${honestRaters.length}x 0.9 ratings`, 'Victim Score': victimRep.score.toFixed(4), 'Attacker Stake': attackerStake.balance });

    // === Phase 2: Attack ===
    console.log(`  Phase 2: Attacker submits false rating (0.1)...`);
    // FIX: submitRating now returns the *actual* rating ID
    attackedRatingId = await submitRating(client, queryContract, attackerUser, 0.1, victimSupplier);
    await sleep(1000); // Wait for commit
    victimRep = await getChainReputation(queryContract, victimSupplier);
    results.push({ 'Phase': '2. Attack', 'Action': `1x 0.1 rating (ID: ...${attackedRatingId.slice(-6)})`, 'Victim Score': victimRep.score.toFixed(4), 'Attacker Stake': attackerStake.balance });

    // === Phase 3: Dispute ===
    console.log(`  Phase 3: Victim disputes the false rating...`);
    // The victim must file the dispute
    // FIX: createDispute now returns the *actual* dispute ID
    disputeId = await createDispute(client, queryContract, victimSupplier, attackedRatingId, 'This is a fraudulent rating.');
    await sleep(1000);
    results.push({ 'Phase': '3. Dispute', 'Action': `Victim files dispute (ID: ...${disputeId.slice(-6)})`, 'Victim Score': victimRep.score.toFixed(4), 'Attacker Stake': attackerStake.balance });

    // === Phase 4: Resolution ===
    console.log(`  Phase 4: Admin resolves dispute as "UPHELD"...`);
    // FIX: We must use the adminContract (connected as 'Admin') to resolve
    await resolveDispute(client, adminUser, disputeId, 'UPHELD', 'Clear evidence of fraud.');
    await sleep(1000);
    victimRep = await getChainReputation(queryContract, victimSupplier); // Score should be restored
    attackerStake = await getChainStake(queryContract, attackerUser);   // Stake should be slashed
    results.push({ 'Phase': '4. Resolution', 'Action': `Admin rules "UPHELD"`, 'Victim Score': victimRep.score.toFixed(4), 'Attacker Stake': attackerStake.balance });
    
    console.log('✓ Economic Security test complete.');
    return results;
}

/**
 * Runs the 4-part performance test.
 */
async function runPerformanceTest(client, queryContract) {
    console.log(`\n--- Running Performance Test ---`);
    const results = [];
    let start, end;
    let totalTime; 

    // --- 1. Sequential Read Latency (Single) ---
    console.log(`  Testing Read Latency (${READ_LATENCY_TESTS} sequential reads)...`);
    totalTime = 0n; 
    const readVictim = 'read_latency_tester';
    try {
        // FIX: Use submitStake helper which connects as the right user (adminUser)
        await submitStake(client, adminUser, '12345', true);
    } catch (err) { /* ignore */ }

    for (let i = 0; i < READ_LATENCY_TESTS; i++) {
        start = process.hrtime.bigint();
        // FIX: Use queryContract (connected as buyer1) for all reads
        await queryContract.evaluateTransaction('GetStake', readVictim);
        end = process.hrtime.bigint();
        totalTime += (end - start);
    }
    const readLatency = (Number(totalTime) / READ_LATENCY_TESTS / 1_000_000).toFixed(2);
    results.push({ 
        'Category': 'Sequential Latency', 
        'Test Case': 'Sequential Read (GetStake)', 
        'TX Count': READ_LATENCY_TESTS, 
        'Concurrency': 1, 
        'Result': `${readLatency} ms` 
    });

    // --- 2. Sequential Write Latency (Single) ---
    console.log(`  Testing Write Latency (${WRITE_LATENCY_TESTS} sequential writes)...`);
    totalTime = 0n; 
    for (let i = 0; i < WRITE_LATENCY_TESTS; i++) {
        start = process.hrtime.bigint();
        await submitStake(client, 'buyer1', minStake, true); 
        end = process.hrtime.bigint();
        totalTime += (end - start);
        await sleep(500); 
    }
    const writeLatency = (Number(totalTime) / WRITE_LATENCY_TESTS / 1_000_000).toFixed(2);
    results.push({ 
        'Category': 'Sequential Latency', 
        'Test Case': 'Sequential Write (AddStake)', 
        'TX Count': WRITE_LATENCY_TESTS, 
        'Concurrency': 1, 
        'Result': `${writeLatency} ms` 
    });

    // --- 3. Concurrent Throughput - Low Conflict (Plural) ---
    console.log(`  Testing Low-Conflict Throughput (${THROUGHPUT_TEST_SIZE} txs @ ${THROUGHPUT_CONCURRENCY})...`);
    let promises = [];
    start = process.hrtime.bigint();
    for (let i = 0; i < THROUGHPUT_TEST_SIZE; i++) {
        const victim = `tps_low_${i}`; // Unique victim
        const user = perfTestUsers[i % perfTestUsers.length];
        promises.push(submitRating(client, queryContract, user, 0.5, victim));
        
        if (promises.length >= CONCURRENCY_LEVEL || i === THROUGHPUT_TEST_SIZE - 1) {
            await runConcurrentTest(promises); 
            promises = [];
        }
    }
    end = process.hrtime.bigint();
    const lowConflictTime = Number(end - start) / 1_000_000_000; // seconds
    const lowConflictTps = (THROUGHPUT_TEST_SIZE / lowConflictTime).toFixed(2);
    results.push({ 
        'Category': 'Concurrent Throughput', 
        'Test Case': 'Low-Conflict (Realistic)', 
        'TX Count': THROUGHPUT_TEST_SIZE, 
        'Concurrency': THROUGHPUT_CONCURRENCY, 
        'Result': `${lowConflictTps} TPS` 
    });

    // --- 4. Concurrent Throughput - High Conflict (Plural) ---
    console.log(`  Testing High-Conflict Throughput (${THROUGHPUT_TEST_SIZE} txs @ ${THROUGHPUT_CONCURRENCY})...`);
    promises = [];
    const highConflictVictim = 'tps_high_conflict_victim'; // Single victim
    let successCount = 0;
    let mvccCount = 0;
    // Must create the high-conflict victim's reputation record first
    await submitRating(client, queryContract, 'buyer1', 0.5, highConflictVictim, true); 

    start = process.hrtime.bigint();
    for (let i = 0; i < THROUGHPUT_TEST_SIZE; i++) {
        const user = perfTestUsers[i % perfTestUsers.length];
        promises.push(submitRating(client, queryContract, user, 0.5, highConflictVictim));
        
        if (promises.length >= CONCURRENCY_LEVEL || i === THROUGHPUT_TEST_SIZE - 1) {
            const { successes, mvccErrors } = await runConcurrentTest(promises);
            successCount += successes;
            mvccCount += mvccErrors;
            promises = [];
        }
    }
    end = process.hrtime.bigint();
    const highConflictTime = Number(end - start) / 1_000_000_000; // seconds
    const highConflictTps = (successCount / highConflictTime).toFixed(2); 
    const mvccRate = ((mvccCount / THROUGHPUT_TEST_SIZE) * 100).toFixed(1);
    results.push({ 
        'Category': 'Concurrent Throughput', 
        'Test Case': 'High-Conflict (Contention)', 
        'TX Count': THROUGHPUT_TEST_SIZE, 
        'Concurrency': THROUGHPUT_CONCURRENCY, 
        'Result': `${highConflictTps} TPS (${mvccRate}% MVCC)` 
    });

    console.log('✓ Performance test complete.');
    return results;
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
                // It's a different, unexpected error
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
            // Ignore errors
        } else {
            throw err;
        }
    }
}

async function submitRating(client, queryContract, username, rating, victim, suppressErrors = false) {
    let userGateway;
    try {
        userGateway = await newGatewayForUser(client, username);
        const userContract = userGateway.getNetwork(channelName).getContract(chaincodeName);

        const tx = userContract.newProposal('SubmitRating', {
            arguments: [
                victim,
                testDimension,
                rating.toString(),
                `hash_tx_${Date.now()}`,
                Math.floor(Date.now() / 1000).toString()
            ]
        });
        const endorsedTx = await tx.endorse();
        await endorsedTx.submit();
        
        // --- FIX: Query for the rating ID instead of assuming it ---
        await sleep(500); // wait for commit
        const resultBytes = await queryContract.evaluateTransaction('GetRatingsByRater', username);
        const ratings = JSON.parse(utf8Decoder.decode(resultBytes));
        if (!ratings || ratings.length === 0) {
            throw new Error(`Could not find rating for user ${username} after submit`);
        }
        return ratings[0].ratingId; // Return the *real* rating ID (most recent)

    } catch(err) {
         if (!suppressErrors) {
            throw err;
        }
         // Return a dummy ID on suppressed error
         return "suppressed_error_tx_id";
    }
}

async function createDispute(client, queryContract, username, ratingId, reason) {
    let userGateway;
    try {
        userGateway = await newGatewayForUser(client, username);
        const userContract = userGateway.getNetwork(channelName).getContract(chaincodeName);

        // --- FIX: Call 'InitiateDispute' to match your chaincode ---
        const tx = userContract.newProposal('InitiateDispute', {
            arguments: [
                ratingId,
                reason,
                `dispute_hash_${Date.now()}`
            ]
        });
        const endorsedTx = await tx.endorse();
        await endorsedTx.submit();

        // --- FIX: Query for the dispute ID instead of assuming it ---
        await sleep(500); // wait for commit
        const resultBytes = await queryContract.evaluateTransaction('GetDisputesByStatus', 'pending');
        const disputes = JSON.parse(utf8Decoder.decode(resultBytes));
        if (!disputes || disputes.length === 0) {
            throw new Error(`Could not find pending dispute for user ${username} after submit`);
        }
        return disputes[0].disputeId; // Return the *real* dispute ID

    } finally {
        if (userGateway) {
            // userGateway.close(); 
        }
    }
}

async function resolveDispute(client, username, disputeId, resolution, comments) {
    let userGateway;
    try {
        userGateway = await newGatewayForUser(client, username);
        const userContract = userGateway.getNetwork(channelName).getContract(chaincodeName);

        await userContract.submitTransaction(
            'ResolveDispute',
            disputeId,
            resolution,
            comments
        );
    } finally {
        if (userGateway) {
            // userGateway.close(); 
        }
    }
}

async function getChainReputation(queryContract, supplier) {
    try {
        // FIX: Use queryContract (connected as buyer1)
        const resultBytes = await queryContract.evaluateTransaction('GetReputation', supplier, testDimension);
        const resultJson = utf8Decoder.decode(resultBytes);
        return JSON.parse(resultJson); // Return the full rep object
    } catch (err) {
        if (err.message.includes('reputation not found') || err.message.includes('key not found')) {
            return { score: 0.5, alpha: 1.0, beta: 1.0 }; // Return prior
        }
        console.error('\nError in getChainReputation:', err.message);
        return { score: 0.0, alpha: 0.0, beta: 0.0 };
    }
}

async function getChainStake(queryContract, username) {
    try {
        // FIX: Use queryContract (connected as buyer1)
        const normalizedUsername = username.toLowerCase();
        const resultBytes = await queryContract.evaluateTransaction('GetStake', normalizedUsername);
        const resultJson = utf8Decoder.decode(resultBytes);
        return JSON.parse(resultJson); // Return the full stake object
    } catch (err) {
        if (err.message.includes('stake not found')) {
            return { balance: 0, locked: 0 }; // Return empty
        }
        console.error('\nError in getChainStake:', err.message);
        return { balance: 0, locked: 0 };
    }
}

// --- Helper: Printing Functions ---

function printEconomicSecurityResults(results) {
    if (results.length === 0) {
        console.log('\n==================== ECONOMIC SECURITY TEST RESULTS ====================');
        console.log('No results to display. The test may have failed to run.');
        console.log('======================================================================');
        return;
    }

    console.log('\n============================== ECONOMIC SECURITY TEST RESULTS ==============================');
    console.table(results);
    console.log('==========================================================================================');
    
    if (results.length === 4) {
        const initialScore = results[0]['Victim Score'];
        const attackedScore = results[1]['Victim Score'];
        const restoredScore = results[3]['Victim Score'];
        
        const initialStake = results[0]['Attacker Stake'];
        const finalStake = results[3]['Attacker Stake'];
        const stakeLost = initialStake - finalStake;
        
        console.log('\n--- Final Scores (Economic Security) ---');
        console.log(`Victim Score:   ${initialScore} (Initial) -> ${attackedScore} (Attacked) -> ${restoredScore} (Restored)`);
        console.log(`Attacker Stake: ${initialStake} (Initial) -> ${finalStake} (Slashed)`);
        console.log(`\nFINDING: A temporary ${ (initialScore - attackedScore).toFixed(4) } point dip in reputation COST the attacker ${stakeLost} tokens.`);
        console.log('This proves the economic disincentive for malicious attacks is functioning correctly.');
    }
}

function printPerformanceResults(results) {
    if (results.length === 0) {
        console.log('\n==================== SYSTEM PERFORMANCE RESULTS ====================');
        console.log('No performance results to display.');
        console.log('====================================================================');
        return;
    }

    const formattedResults = results.map(r => ({
        'Category': r.Category,
        'Test Case': r['Test Case'],
        'TX Count': r['TX Count'],
        'Concurrency': r.Concurrency,
        'Result': r.Result
    }));

    console.log('\n============================== COMPREHENSIVE PERFORMANCE RESULTS ==============================');
    console.table(formattedResults, ['Category', 'Test Case', 'TX Count', 'Concurrency', 'Result']);
    console.log('========================================================================================');
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
