This is a demo of intent signing that we need to integrate before placing any orders.

1. Users will sign orders in metamask. We need to store data and signature of their order in this below format (we can add more fields if needed)

"expiry": 1741893164,
    "name": "ETH",
    "orderType": "BUY",
    "position": "1.5",
    "signature": "0xe4f0461fce1a4a036b970070e09201283bb816acf9888cb9c516cae07211a0252d06a4da26f6f555fc7a0e3294fc41d169a04f03fdc95c97e021c253074b55d81b",
    "value": "2000"
  },

2. A .json file with name like this trades_2025-03-13_00-43-36 will be created for setted time interval (time interval is set as 1 min for testing. It will be 24 hrs). All trades will be pushed to this file till 24 hrs is complete and new file will be created after that.

3. This file will be uploaded to IPFS on completion of time interval. We get CID from IPFS.

4. Call intentBatchEmit in smart contract (this will be same USER_MANAGER address but for testing I deployed new one ) and provide (start timestamp, end timestamp, cid) and this will emit an event. It will retry for 2 times (needs improvement in retrying logic)
