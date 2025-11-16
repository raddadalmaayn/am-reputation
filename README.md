# Blockchain Reputation System for Additive Manufacturing Supply Chains

A decentralized reputation framework for additive manufacturing networks built on Hyperledger Fabric. This system combines Bayesian statistical modeling with economic incentives to create trustworthy, manipulation-resistant supplier ratings.

## Overview

Additive manufacturing increasingly relies on distributed networks of suppliers. When buyers need to select manufacturers they've never worked with, they face a trust problem: how do you know if a supplier will deliver quality parts on time?

This system solves that problem by maintaining an immutable, decentralized ledger of supplier performance ratings. Unlike centralized platforms where the operator controls all data, our blockchain-based approach ensures no single party can manipulate or delete reputation history.

## Key Features

### Bayesian Reputation Model
- Uses Beta distributions to model reputation as probability distributions rather than single scores
- Provides confidence intervals showing uncertainty (e.g., "0.85 ± 0.12")
- Handles new suppliers gracefully with principled priors
- Updates in constant time O(1) regardless of rating history

### Multi-Dimensional Assessment
Suppliers are rated separately across multiple dimensions:
- **Quality**: Dimensional accuracy, surface finish, material properties
- **Delivery**: On-time performance, lead time consistency
- **Compliance**: Certifications, documentation, regulatory adherence
- **Warranty**: Post-delivery support, defect handling

This prevents a supplier who excels in quality but struggles with delivery from hiding behind a single averaged score.

### Economic Security
Five mechanisms protect against manipulation:

1. **Stake Requirements**: Participants must deposit tokens before rating others
2. **Self-Rating Prevention**: Identity verification prevents actors from rating themselves
3. **Access Control**: Admin functions require proper authorization
4. **Duplicate Prevention**: Each actor can rate another only once per dimension
5. **Evidence Hashing**: SHA-256 hashes prove evidence hasn't been tampered with

### Temporal Decay
Reputation scores gradually decay over time, ensuring recent performance matters more than ancient history. A supplier who was excellent two years ago but has declined won't keep their old high score indefinitely.

## Performance

Tested on a 4-peer Hyperledger Fabric network (2 organizations, 2 peers each):

| Operation | Metric | Result |
|-----------|--------|--------|
| Sequential Read | Latency | 6.63 ms |
| Sequential Write | Latency | 99.86 ms |
| Concurrent Read | Throughput | 612.94 TPS |
| Concurrent Write (Low Conflict) | Throughput | 113.48 TPS |
| Concurrent Write (High Conflict) | Throughput | 1.54 TPS |
| Storage | Per Transaction | 220 bytes |

The system correctly handles MVCC conflicts under contention, rejecting 99% of conflicting transactions to maintain data consistency.

## Security Validation

### Adversarial Testing
We systematically tested the system against five attack types:

- **Insufficient Stake Attack**: Blocked (100%)
- **Self-Rating Attack**: Blocked (100%)
- **Unauthorized Admin Access**: Blocked (100%)
- **Evidence Tampering**: Blocked (100%)
- **Sybil Attack**: Economically deterred (partial block)

Overall prevention rate: **83.3%**

This testing revealed a critical bug in identity normalization where Base64-encoded X.509 certificates weren't being properly parsed. The bug was fixed and validated.

### Sybil Attack Resilience
When subjected to a coordinated attack by 10 fake identities attempting to manipulate a supplier's reputation:

- **Naive averaging**: Reputation dropped 30% (0.90 → 0.63)
- **Our Bayesian model**: Reputation dropped 14% (0.86 → 0.74)

This represents a **2.1× improvement** in attack resistance. The Bayesian prior acts as a statistical defense, treating sudden bursts of conflicting ratings as potential outliers rather than definitive truth.

## Installation

### Prerequisites
- Docker 20.10+
- Docker Compose 1.29+
- Node.js 16+
- Go 1.19+
- Hyperledger Fabric 2.5

### Setup

1. Clone the repository:
```bash
git clone https://github.com/raddadalmaayn/am-reputation.git
cd am-reputation
```

2. Start the Fabric test network:
```bash
cd fabric-samples/test-network
./network.sh up createChannel -c mychannel -ca
```

3. Deploy the chaincode:
```bash
./network.sh deployCC -ccn repcc -ccp ../../am-reputation/chaincode -ccl go
```

4. Install client dependencies:
```bash
cd ../../am-reputation/client-tests
npm install
```

5. Enroll test users:
```bash
chmod +x enroll_test_users.sh
./enroll_test_users.sh
```

## Usage

### Basic Operations

**Add stake** (required before rating):
```javascript
const { Gateway, Wallets } = require('fabric-network');

const wallet = await Wallets.newFileSystemWallet('./wallet');
const gateway = new Gateway();
await gateway.connect(connectionProfile, {
    wallet,
    identity: 'buyer1',
    discovery: { enabled: true, asLocalhost: true }
});

const network = await gateway.getNetwork('mychannel');
const contract = network.getContract('repcc');

await contract.submitTransaction('AddStake', '15000');
```

**Submit a rating**:
```javascript
const crypto = require('crypto');
const fs = require('fs');

// Hash evidence (quality inspection report, photos, etc.)
const evidence = fs.readFileSync('inspection_report.pdf');
const evidenceHash = crypto.createHash('sha256')
    .update(evidence)
    .digest('hex');

await contract.submitTransaction(
    'SubmitRating',
    'supplier_XYZ',           // Who you're rating
    'quality',                // Dimension
    '0.92',                   // Rating (0 to 1)
    evidenceHash,             // Evidence hash
    Date.now().toString()     // Timestamp
);
```

**Query reputation**:
```javascript
const result = await contract.evaluateTransaction(
    'GetReputation',
    'supplier_XYZ',
    'quality'
);

const reputation = JSON.parse(result.toString());
console.log(`Score: ${reputation.score}`);
console.log(`Confidence: [${reputation.ci_lower}, ${reputation.ci_upper}]`);
console.log(`Total ratings: ${reputation.totalEvents}`);
```

### Running Tests

**Performance benchmarks**:
```bash
cd client-tests
node performance_test.js
```

**Resilience test** (Sybil attack simulation):
```bash
node resilience_results.js
```

**Full test suite**:
```bash
./run_full_test.sh
```

## Architecture

### Chaincode Structure
```
chaincode/
├── contract.go          # Main smart contract logic
├── go.mod              # Go dependencies
└── go.sum              # Dependency checksums
```

### Data Model

**Reputation Record**:
```go
type Reputation struct {
    ActorID     string  // Supplier identifier
    Dimension   string  // quality, delivery, etc.
    Alpha       float64 // Beta distribution parameter
    Beta        float64 // Beta distribution parameter
    TotalEvents int     // Number of ratings received
    LastTs      int64   // Last update timestamp
}
```

**Rating Record**:
```go
type Rating struct {
    RatingID  string  // Unique rating identifier
    RaterID   string  // Who submitted the rating
    ActorID   string  // Who was rated
    Dimension string  // Which dimension
    Value     float64 // Rating value [0, 1]
    Weight    float64 // Rater's influence weight
    Evidence  string  // SHA-256 hash of evidence
    Timestamp int64   // Submission time
    TxID      string  // Blockchain transaction ID
}
```

**Stake Record**:
```go
type Stake struct {
    ActorID   string  // Participant identifier
    Balance   float64 // Available tokens
    Locked    float64 // Tokens locked in disputes
    UpdatedAt int64   // Last modification time
}
```

### Smart Contract Functions

**Governance**:
- `InitConfig()` - Initialize system parameters
- `UpdateConfig()` - Modify system settings (admin only)
- `UpdateDecayRate()` - Adjust temporal decay rate
- `AddDimension()` - Add new reputation dimension

**Stake Management**:
- `AddStake(amount)` - Deposit tokens
- `GetStake(actorId)` - Query stake balance

**Rating Operations**:
- `SubmitRating(actorId, dimension, value, evidence, timestamp)` - Submit rating
- `GetReputation(actorId, dimension)` - Query reputation with decay applied
- `GetRatingHistory(actorId, dimension)` - Retrieve all ratings

**Dispute Resolution**:
- `InitiateDispute(ratingId, reason)` - Challenge a rating
- `ResolveDispute(disputeId, verdict, notes)` - Admin resolution

**Queries**:
- `GetActorsByDimension(dimension, minScore)` - Find qualified suppliers
- `GetRatingsByRater(raterId)` - Audit a rater's submissions
- `GetDisputesByStatus(status)` - List open/resolved disputes

## Mathematical Foundation

### Bayesian Update
When a rating `r` is submitted:
```
α' = α + r
β' = β + (1 - r)
score = α' / (α' + β')
```

Where:
- α accumulates positive evidence
- β accumulates negative evidence
- The score is the expected value of the Beta distribution

### Confidence Intervals
We use Wilson score intervals to calculate uncertainty:
```
n = α + β
p = α / n
z = 1.96  (for 95% confidence)

center = (p + z²/2n) / (1 + z²/n)
margin = (z / (1 + z²/n)) × √(p(1-p)/n + z²/4n²)

CI = [center - margin, center + margin]
```

### Temporal Decay
Reputation decays exponentially toward the prior:
```
decay_factor = λ^(Δt / T)

α_effective = α_prior + (α - α_prior) × decay_factor
β_effective = β_prior + (β - β_prior) × decay_factor
```

Where:
- λ = decay rate (default: 0.98)
- Δt = time since last update
- T = decay period (default: 86400 seconds = 1 day)

## Configuration

System parameters (modifiable via governance):
```go
MinStakeRequired: 10000.0    // Minimum tokens to participate
DisputeCost: 100.0           // Cost to file a dispute
SlashPercentage: 0.1         // Stake lost if dispute overturned
DecayRate: 0.98              // Daily decay factor
DecayPeriod: 86400.0         // Decay period in seconds
InitialAlpha: 2.0            // Bayesian prior parameter
InitialBeta: 2.0             // Bayesian prior parameter
```

## Development

### Running locally

1. Start the development network:
```bash
cd fabric-samples/test-network
./network.sh down
./network.sh up createChannel -c mychannel -ca
```

2. Deploy your modified chaincode:
```bash
./network.sh deployCC -ccn repcc -ccp /path/to/chaincode -ccl go
```

3. Run tests:
```bash
cd client-tests
npm test
```

### Project structure
```
am-reputation/
├── chaincode/           # Go smart contract
│   └── contract.go
├── client-tests/        # Node.js test clients
│   ├── performance_test.js
│   ├── resilience_results.js
│   └── final_test.js
├── .gitignore
└── README.md
```

## Known Limitations

1. **Vendor dependencies committed**: The `chaincode/vendor/` directory contains auto-generated code. In production, use `go.mod` and omit vendor from version control.

2. **Sybil attacks partially effective**: While economically deterred, an attacker with sufficient capital can still create multiple identities. Future work could integrate CA-level restrictions or reputation-weighted voting.

3. **Temporal decay applied at query time**: For performance reasons, decay is calculated when reputation is queried rather than continuously updated. This means stored α/β values don't reflect decay until accessed.

4. **MVCC conflicts under high contention**: When many transactions target the same reputation record simultaneously, most will fail with MVCC conflicts and require retry. This is correct behavior but impacts throughput.

## Future Enhancements

- **Machine learning anomaly detection**: Flag suspicious rating patterns automatically
- **Graph-based trust propagation**: Leverage network structure (e.g., EigenTrust)
- **Zero-knowledge proofs**: Prove evidence exists without revealing proprietary data
- **Cross-chain bridges**: Enable reputation portability across blockchain platforms
- **Federated learning**: Share reputation models without sharing raw data

## Contributing

We welcome contributions. Please:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/your-feature`)
3. Commit your changes (`git commit -m 'Add some feature'`)
4. Push to the branch (`git push origin feature/your-feature`)
5. Open a Pull Request


## Acknowledgments

- Built on Hyperledger Fabric
- Inspired by EigenTrust and Beta reputation systems
- Adversarial testing methodology adapted from blockchain security literature

## Contact

For questions or collaboration inquiries, contact [raddadalmaayn@unm.edu] or open an issue on GitHub.

---

**Note**: This is research software. Use in production environments requires additional security auditing, key management infrastructure, and operational considerations not covered in this implementation.
