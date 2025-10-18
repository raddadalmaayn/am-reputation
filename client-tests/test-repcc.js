'use strict';

const { Gateway, Wallets } = require('fabric-network');
const path = require('path');
const fs = require('fs');

// Connection profile
const ccpPath = path.resolve(__dirname, '..', '..', 'fabric-samples', 'test-network', 'organizations', 'peerOrganizations', 'org1.example.com', 'connection-org1.json');
const ccp = JSON.parse(fs.readFileSync(ccpPath, 'utf8'));

// Wallet path
const walletPath = path.join(process.cwd(), 'wallet');

async function main() {
    try {
        console.log('=== Reputation System Test Suite ===\n');

        // Load wallet
        const wallet = await Wallets.newFileSystemWallet(walletPath);
        
        // Check for admin identity
        const identity = await wallet.get('admin1');
        if (!identity) {
            console.log('Admin identity not found in wallet');
            console.log('Run: node enrollAdmin.js first');
            return;
        }

        // Connect to gateway
        const gateway = new Gateway();
        await gateway.connect(ccp, {
            wallet,
            identity: 'admin1',
            discovery: { enabled: true, asLocalhost: true }
        });

        // Get network and contract
        const network = await gateway.getNetwork('mychannel');
        const contract = network.getContract('repcc');

        console.log('✓ Connected to network\n');

        // Test 1: Get Config
        console.log('Test 1: Get System Configuration');
        const config = await contract.evaluateTransaction('GetConfig');
        const configObj = JSON.parse(config.toString());
        console.log('  Min Stake Required:', configObj.minStakeRequired);
        console.log('  Valid Dimensions:', Object.keys(configObj.validDimensions).join(', '));
        console.log('  Decay Rate:', configObj.decayRate);
        console.log('  ✓ Config retrieved\n');

        // Test 2: Add Stake
        console.log('Test 2: Add Stake');
        await contract.submitTransaction('AddStake', '50000');
        console.log('  ✓ Stake added: 50,000\n');

        // Test 3: Submit Ratings
        console.log('Test 3: Submit Multiple Ratings');
        const actors = ['supplier_A', 'supplier_B', 'supplier_C'];
        const dimensions = ['quality', 'delivery', 'compliance'];
        
        let ratingCount = 0;
        const startTime = Date.now();

        for (const actor of actors) {
            for (const dimension of dimensions) {
                const value = (0.7 + Math.random() * 0.3).toFixed(2);
                const timestamp = Math.floor(Date.now() / 1000).toString();
                
                try {
                    const result = await contract.submitTransaction(
                        'SubmitRating',
                        actor,
                        dimension,
                        value,
                        `evidence_${ratingCount}`,
                        timestamp
                    );
                    ratingCount++;
                    process.stdout.write('.');
                } catch (error) {
                    if (error.message.includes('self-rating')) {
                        console.log('\n  ⚠ Self-rating blocked (expected)');
                    } else {
                        console.error('\n  Error:', error.message);
                    }
                }
            }
        }

        const elapsed = ((Date.now() - startTime) / 1000).toFixed(2);
        console.log(`\n  ✓ Submitted ${ratingCount} ratings in ${elapsed}s\n`);

        // Test 4: Query Reputations
        console.log('Test 4: Query Reputations');
        for (const actor of actors) {
            for (const dimension of dimensions) {
                const rep = await contract.evaluateTransaction('GetReputation', actor, dimension);
                const repObj = JSON.parse(rep.toString());
                
                console.log(`  ${actor} (${dimension}):`, {
                    score: repObj.score.toFixed(3),
                    events: repObj.totalEvents,
                    ci: `[${repObj.ci_lower.toFixed(3)}, ${repObj.ci_upper.toFixed(3)}]`
                });
            }
        }
        console.log('  ✓ Reputations retrieved\n');

        // Test 5: Performance Test
        console.log('Test 5: Performance Test (100 ratings)');
        const perfStart = Date.now();
        const perfRatings = [];

        for (let i = 0; i < 100; i++) {
            const actor = actors[i % actors.length];
            const dimension = dimensions[i % dimensions.length];
            const value = (0.6 + Math.random() * 0.4).toFixed(2);
            const timestamp = Math.floor(Date.now() / 1000).toString();
            
            perfRatings.push(
                contract.submitTransaction(
                    'SubmitRating',
                    actor,
                    dimension,
                    value,
                    `perf_evidence_${i}`,
                    timestamp
                ).catch(err => {
                    if (!err.message.includes('self-rating')) {
                        console.error('Error:', err.message);
                    }
                })
            );
            
            if ((i + 1) % 10 === 0) {
                process.stdout.write(`${i + 1}...`);
            }
        }

        await Promise.all(perfRatings);
        const perfElapsed = ((Date.now() - perfStart) / 1000).toFixed(2);
        const tps = (100 / perfElapsed).toFixed(2);
        
        console.log(`\n  ✓ Performance: ${tps} TPS\n`);

        // Test 6: Final Statistics
        console.log('Test 6: Final Statistics');
        for (const actor of actors) {
            console.log(`\n  ${actor}:`);
            for (const dimension of dimensions) {
                const rep = await contract.evaluateTransaction('GetReputation', actor, dimension);
                const repObj = JSON.parse(rep.toString());
                
                console.log(`    ${dimension}: ${repObj.score.toFixed(3)} (${repObj.totalEvents} events)`);
            }
        }

        console.log('\n=== All Tests Complete ===');
        
        // Disconnect
        gateway.disconnect();

    } catch (error) {
        console.error(`\n❌ Error: ${error.message}`);
        if (error.stack) {
            console.error(error.stack);
        }
        process.exit(1);
    }
}

main();
