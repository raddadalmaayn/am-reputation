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
