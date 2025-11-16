'use strict';

const FabricCAServices = require('fabric-ca-client');
const { Wallets } = require('fabric-network');
const fs = require('fs');
const path = require('path');

async function main() {
    try {
        // Load connection profile
        const ccpPath = path.resolve(__dirname, '..', '..', 'fabric-samples', 'test-network', 'organizations', 'peerOrganizations', 'org1.example.com', 'connection-org1.json');
        const ccp = JSON.parse(fs.readFileSync(ccpPath, 'utf8'));

        // Create CA client
        const caInfo = ccp.certificateAuthorities['ca.org1.example.com'];
        const caTLSCACerts = caInfo.tlsCACerts.pem;
        const ca = new FabricCAServices(caInfo.url, { trustedRoots: caTLSCACerts, verify: false }, caInfo.caName);

        // Create wallet
        const walletPath = path.join(process.cwd(), 'wallet');
        const wallet = await Wallets.newFileSystemWallet(walletPath);

        // Check if admin already enrolled
        const identity = await wallet.get('admin1');
        if (identity) {
            console.log('✓ Admin identity already exists in wallet');
            return;
        }

        // Enroll admin
        console.log('Enrolling admin1...');
        const enrollment = await ca.enroll({
            enrollmentID: 'admin1',
            enrollmentSecret: 'admin1pw',
            attr_reqs: [{ name: 'admin', optional: false }]
        });

        const x509Identity = {
            credentials: {
                certificate: enrollment.certificate,
                privateKey: enrollment.key.toBytes(),
            },
            mspId: 'Org1MSP',
            type: 'X.509',
        };

        await wallet.put('admin1', x509Identity);
        console.log('✓ Successfully enrolled admin1 and imported to wallet');

    } catch (error) {
        console.error(`Failed to enroll admin: ${error.message}`);
        process.exit(1);
    }
}

main();
