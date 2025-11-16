/*
 * FINAL COMPREHENSIVE TEST SUITE
 * Measures:
 * 1. Resilience: Bayesian vs. Naive models under a Sybil attack.
 * 2. Performance: Sequential Latency (single) and Concurrent Throughput (plural).
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

// --- Resilience Test Config ---
const victimSupplier = `supplier_V_${Date.now()}`;
const testDimension = 'quality';
const goodRating = 0.9;
const badRating = 0.1;
const buyers = Array.from({ length: 20 }, (_, i) => `buyer${i + 1}`);
const attackers = Array.from({ length: 10 }, (_, i) => `attacker${i + 1}`);

// --- Performance Test Config ---
const perfUsers = Array.from({ length: 30 }, (_, i) => `tps_user_${i + 1}`);
const allTestUsers = [victimSupplier, ...buyers, ...attackers, ...perfUsers];
const READ_LATENCY_TESTS = 100;
const WRITE_LATENCY_TESTS = 100;
const THROUGHPUT_TEST_SIZE = 500;
const THROUGHPUT_CONCURRENCY = 100;

// --- Connection Details ---
const cryptoPath = path.resolve(__dirname, '..', '..', 'fabric-samples', 'test-network', 'organizations', 'peerOrganizations', 'org1.example.com');
const peerEndpoint = 'localhost:7051';
const peerHostAlias = 'peer0.org1.example.com';
const tlsCertPath = path.resolve(cryptoPath, 'peers', 'peer0.org1.example.com', 'tls', 'ca.crt');

// --- Simulation Models ---
class NaiveModel {
    constructor() {
        this.sum = 0;
        this.count = 0;
        this.score = 0.5;
    }
    addRating(rating) {
        this.sum += rating;
        this.count += 1;
        this.score = this.sum / this.count;
        return this.score;
    }
}
class BayesianModelNoDecay {
    constructor() {
        this.alpha = 1.0;
        this.beta = 1.0;
        this.score = 0.5;
    }
    addRating(rating) {
        this.alpha += rating;
        this.beta += (1.0 - rating);
        this.score = this.alpha / (this.alpha + this.beta);
        return this.score;
    }
}

// --- Main Test Function ---
async function main() {
    console.log('--- Starting Comprehensive Test Suite (Resilience + Performance) ---');
    
    const resilienceResults = [];
    const performanceResults = [];
    
    const adminClient = await newGrpcConnection();
    let adminGateway;
    let contract;

    try {
        adminGateway = await newGatewayForUser(adminClient, 'Admin');
        const network = adminGateway.getNetwork(channelName);
        contract = network.getContract(chaincodeName);
        console.log('✓ Connected as admin');

        await setupTestUsers(adminClient);

        const resResults = await runResilienceTest(adminClient, contract);
        resilienceResults.push(...resResults);

        const perfResults = await runPerformanceTest(adminClient, contract);
        performanceResults.push(...perfResults);

    } catch (error) {
        console.error('\n*** TOP LEVEL TEST FAILED ***');
        console.error(error);
    } finally {
        if (adminGateway) {
            adminGateway.close();
        }
        adminClient.close();
        
        printResilienceResults(resilienceResults);
        printPerformanceResults(performanceResults);
        
        console.log('\n--- Comprehensive Test Suite Complete ---');
    }
}

// --- Test Logic Functions ---

async function setupTestUsers(client) {
    console.log(`\n--- Setup Phase: Adding ${minStake} stake to all ${allTestUsers.length} test users ---`);
    for (let i = 0; i < allTestUsers.length; i++) {
        const username = allTestUsers[i];
        process.stdout.write(`  Staking user ${i + 1}/${allTestUsers.length} (${username})...`);
        
        let userGateway;
        try {
            userGateway = await newGatewayForUser(client, username);
            const userContract = userGateway.getNetwork(channelName).getContract(chaincodeName);
            
            try {
                await userContract.submitTransaction('AddStake', minStake);
                process.stdout.write('✓\n');
                await sleep(1000); // 1s wait
            } catch (err) {
                if (err.message.includes('stake already exists')) {
                     process.stdout.write('✓ (Stake already exists)\n');
                } else {
                    process.stdout.write(' (1st attempt failed, retrying...)');
                    await sleep(2000); 
                    await userContract.submitTransaction('AddStake', minStake);
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


async function runResilienceTest(client, adminContract) {
    console.log(`\n--- Running Resilience Test ---`);
    console.log(`Victim: ${victimSupplier} | Dimension: ${testDimension}`);
    const results = [];
    const naiveModel = new NaiveModel();
    const bayesNoDecayModel = new BayesianModelNoDecay();

    console.log(`--- Phase 1: Submitting ${buyers.length} good ratings (0.9) ---`);
    for (let i = 0; i < buyers.length; i++) {
        const username = buyers[i];
        process.stdout.write(`  Submitting tx ${i + 1} (user ${username})...`);
        await submitRating(client, username, goodRating);
        await sleep(500); 

        const chainScore = await getChainReputation(adminContract);
        const naiveScore = naiveModel.addRating(goodRating);
        const bayesNoDecayScore = bayesNoDecayModel.addRating(goodRating);
        results.push({
            'Phase': '1: Establishment', 'TX #': i + 1, 'Rater ID': username, 'Rating': goodRating,
            'Score (Naive)': naiveScore, 'Score (Bayes, No Decay)': bayesNoDecayScore, 'Score (Bayes + Decay)': chainScore,
        });
        process.stdout.write('✓\n');
    }
    console.log('✓ Phase 1 complete.');

    console.log(`\n--- Phase 2: Submitting ${attackers.length} bad ratings (0.1) ---`);
    for (let i = 0; i < attackers.length; i++) {
        const username = attackers[i];
        process.stdout.write(`  Submitting tx ${i + buyers.length + 1} (user ${username})...`);
        await submitRating(client, username, badRating);
        await sleep(500); 

        const chainScore = await getChainReputation(adminContract);
        const naiveScore = naiveModel.addRating(badRating);
        const bayesNoDecayScore = bayesNoDecayModel.addRating(badRating);
        results.push({
            'Phase': '2: Attack', 'TX #': i + buyers.length + 1, 'Rater ID': username, 'Rating': badRating,
            'Score (Naive)': naiveScore, 'Score (Bayes, No Decay)': bayesNoDecayScore, 'Score (Bayes + Decay)': chainScore,
        });
        process.stdout.write('✓\n');
    }
    console.log('✓ Phase 2 complete.');
    return results;
}

async function runPerformanceTest(client, adminContract) {
    console.log(`\n--- Running Performance Test ---`);
    const results = [];
    let start, end, totalTime; 

    // --- 1. Sequential Read Latency (Single) ---
    console.log(`  Testing Read Latency (${READ_LATENCY_TESTS} sequential reads)...`);
    totalTime = 0n; 
    for (let i = 0; i < READ_LATENCY_TESTS; i++) {
        start = process.hrtime.bigint();
        await adminContract.evaluateTransaction('GetReputation', victimSupplier, testDimension);
        end = process.hrtime.bigint();
        totalTime += (end - start);
    }
    const readLatency = (Number(totalTime) / READ_LATENCY_TESTS / 1_000_000).toFixed(2);
    results.push({ 
        'Category': 'Sequential Latency', 'Test Case': 'Sequential Read (GetRep)', 
        'TX Count': READ_LATENCY_TESTS, 'Concurrency': 1, 'Result': `${readLatency} ms` 
    });

    // --- 2. Sequential Write Latency (Single) ---
    console.log(`  Testing Write Latency (${WRITE_LATENCY_TESTS} sequential writes)...`);
    totalTime = 0n; 
    for (let i = 0; i < WRITE_LATENCY_TESTS; i++) {
        start = process.hrtime.bigint();
        await submitStake(client, 'buyer1', `1000${i}`); 
        end = process.hrtime.bigint();
        totalTime += (end - start);
        await sleep(500); // Prevent race conditions
    }
    const writeLatency = (Number(totalTime) / WRITE_LATENCY_TESTS / 1_000_000).toFixed(2);
    results.push({ 
        'Category': 'Sequential Latency', 'Test Case': 'Sequential Write (AddStake)', 
        'TX Count': WRITE_LATENCY_TESTS, 'Concurrency': 1, 'Result': `${writeLatency} ms` 
    });

    // --- 3. Concurrent Throughput - Low Conflict (Plural) ---
    console.log(`  Testing Low-Conflict Throughput (${THROUGHPUT_TEST_SIZE} txs @ ${THROUGHPUT_CONCURRENCY})...`);
    let promises = [];
    start = process.hrtime.bigint();
    for (let i = 0; i < THROUGHPUT_TEST_SIZE; i++) {
        const victim = `tps_low_${i}`; 
        const user = perfUsers[i % perfUsers.length];
        promises.push(submitRating(client, user, 0.5, victim));
        
        if (promises.length >= THROUGHPUT_CONCURRENCY || i === THROUGHPUT_TEST_SIZE - 1) {
            await runConcurrentTest(promises); 
            promises = [];
        }
    }
    end = process.hrtime.bigint();
    const lowConflictTime = Number(end - start) / 1_000_000_000; // seconds
    const lowConflictTps = (THROUGHPUT_TEST_SIZE / lowConflictTime).toFixed(2);
    results.push({ 
        'Category': 'Concurrent Throughput', 'Test Case': 'Low-Conflict (Realistic)', 
        'TX Count': THROUGHPUT_TEST_SIZE, 'Concurrency': THROUGHPUT_CONCURRENCY, 'Result': `${lowConflictTps} TPS` 
    });

    // --- 4. Concurrent Throughput - High Conflict (Plural) ---
    console.log(`  Testing High-Conflict Throughput (${THROUGHPUT_TEST_SIZE} txs @ ${THROUGHPUT_CONCURRENCY})...`);
    promises = [];
    const highConflictVictim = 'tps_high_conflict_victim'; // Single victim
    let successCount = 0;
    let mvccCount = 0;
    start = process.hrtime.bigint();
    for (let i = 0; i < THROUGHPUT_TEST_SIZE; i++) {
        const user = perfUsers[i % perfUsers.length];
        promises.push(submitRating(client, user, 0.5, highConflictVictim));
        
        if (promises.length >= THROUGHPUT_CONCURRENCY || i === THROUGHPUT_TEST_SIZE - 1) {
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
        'Category': 'Concurrent Throughput', 'Test Case': 'High-Conflict (Contention)', 
        'TX Count': THROUGHPUT_TEST_SIZE, 'Concurrency': THROUGHPUT_CONCURRENCY, 'Result': `${highConflictTps} TPS (${mvccRate}% MVCC)` 
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
                console.error('\nUnexpected error during concurrent test:', result.reason);
            }
        }
    }
    return { successes, mvccErrors };
}


// --- Helper: Submit Functions ---
async function submitStake(client, username, amount) {
    let userGateway;
    try {
        userGateway = await newGatewayForUser(client, username);
        const userContract = userGateway.getNetwork(channelName).getContract(chaincodeName);
        await userContract.submitTransaction('AddStake', amount);
    } catch(err) {
        if (!err.message.includes('stake already exists') && !err.message.includes('failed to endorse transaction')) {
            throw err;
        }
    }
    finally {
        if (userGateway) {
            // userGateway.close(); 
        }
    }
}

async function submitRating(client, username, rating, victim = victimSupplier) {
    let userGateway;
    try {
        userGateway = await newGatewayForUser(client, username);
        const userContract = userGateway.getNetwork(channelName).getContract(chaincodeName);
        await userContract.submitTransaction(
            'SubmitRating',
            victim, 
            testDimension,
            rating.toString(),
            `hash_tx_${Date.now()}`,
            Math.floor(Date.now() / 1000).toString()
        );
    } finally {
        if (userGateway) {
            // userGateway.close(); 
        }
    }
}

async function getChainReputation(adminContract) {
    try {
        const resultBytes = await adminContract.evaluateTransaction('GetReputation', victimSupplier, testDimension);
        const resultJson = utf8Decoder.decode(resultBytes);
        const result = JSON.parse(resultJson);
        if (result && typeof result.score === 'number') {
             return result.score;
        }
        return 0.5; // Return prior
    } catch (err) {
        if (err.message.includes('reputation not found') || err.message.includes('key not found')) {
            return 0.5; 
        }
        console.error('\nError in getChainReputation:', err.message);
        return 0.0;
    }
}

// --- Helper: Printing Functions ---
function printResilienceResults(results) {
    if (results.length === 0) {
        console.log('\n==================== COMPARATIVE RESILIENCE TEST RESULTS ====================');
        console.log('No results to display. The test may have failed to run.');
        console.log('=============================================================================');
        return;
    }

    const formattedResults = results.map(r => ({
        'Phase': r['Phase'],
        'TX #': r['TX #'],
        'Rater ID': r['Rater ID'],
        'Rating': r['Rating'],
        'Score (Naive)': parseFloat(r['Score (Naive)']).toFixed(4),
        'Score (Bayes, No Decay)': parseFloat(r['Score (Bayes, No Decay)']).toFixed(4),
        'Score (Bayes + Decay)': typeof r['Score (Bayes + Decay)'] === 'number' ? r['Score (Bayes + Decay)'].toFixed(4) : 'N/A',
    }));

    console.log('\n================================== COMPARATIVE RESILIENCE TEST RESULTS ==================================');
    console.table(formattedResults);
    console.log('=========================================================================================================');
    
    if (results.length >= (buyers.length + attackers.length)) {
        const finalNaive = parseFloat(results[results.length - 1]['Score (Naive)']);
        const finalBayesNoDecay = parseFloat(results[results.length - 1]['Score (Bayes, No Decay)']);
        const finalBayesWithDecay = parseFloat(results[results.length - 1]['Score (Bayes + Decay)']);
        
        const startAttackNaive = parseFloat(results[buyers.length - 1]['Score (Naive)']);
        const startAttackBayesNoDecay = parseFloat(results[buyers.length - 1]['Score (Bayes, No Decay)']);
        const startAttackBayesWithDecay = parseFloat(results[buyers.length - 1]['Score (Bayes + Decay)']);

        const naiveDrop = startAttackNaive - finalNaive;
        const bayesNoDecayDrop = startAttackBayesNoDecay - finalBayesNoDecay;
        const bayesWithDecayDrop = startAttackBayesWithDecay - finalBayesWithDecay;
        
        const resiliencePercent = ((naiveDrop - bayesNoDecayDrop) / naiveDrop) * 100;

        console.log('\n--- Final Scores (Resilience) ---');
        console.log(`Naive System (Baseline):     ${finalNaive.toFixed(4)} (Dropped ${naiveDrop.toFixed(4)} points)`);
        console.log(`Bayesian (No Decay) System:  ${finalBayesNoDecay.toFixed(4)} (Dropped ${bayesNoDecayDrop.toFixed(4)} points)`);
        console.log(`On-Chain (Bayes + Decay):    ${finalBayesWithDecay.toFixed(4)} (Dropped ${bayesWithDecayDrop.toFixed(4)} points)`);
        console.log(`\nFINDING: Your Bayesian model was ${resiliencePercent.toFixed(1)}% more resilient to attack than the Naive model.`);
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

    console.log('\n============================== SYSTEM PERFORMANCE RESULTS ==============================');
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
