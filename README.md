# Requirements Summary — Distributed Auction System

## 1. System Overview
- Implement a **distributed auction system** using **replication**.
- The system must behave according to the auction semantics specified.
- Must tolerate **at least one crash failure** (failure-stop model).



---

## 2. System Architecture Requirements
- Implement the system as **multiple nodes** (processes; *no threads*).
- Each node runs in a **distinct process**.
- Clients may send API requests to **any node**.
- You decide how many nodes a client may know.

---

## 3. API Requirements

### `bid(amount)`
- **Input:** `amount` (int)  
- **Output:** acknowledgment with outcome ∈ {`fail`, `success`, `exception`}  
- **Meaning:** Submit a bid to the auction.

### `result()`
- **Input:** none  
- **Output:** outcome describing either:
  - The final result (if auction is over), or  
  - The current highest bid.

---

## 4. Auction Semantics
The system must satisfy the following behaviors under any reasonable interleaving of requests:

1. **First `bid` registers the bidder.**
2. Bidders may **bid multiple times**, but **each new bid must be higher** than all their previous bids.
3. After a **predefined time window** (e.g., 100 time units from system start),  
   → The **highest bidder wins** the auction.  
4. Clients may call `result()` at any time to query:
   - Current auction state (if still running), or
   - Final winner (after auction closes).

---

## 5. Fault Tolerance Assumptions and Requirements

### Network Model
- **Reliable and ordered** message transport.
- Messages to non-failed nodes **always complete within a known time bound**.

### Failure Model
- Nodes may suffer **failure-stop** crashes.
- The system must remain **operational and semantically correct** despite the crash of **one (1) node**.

---
