# faucet
Alpha faucet website

You must have an instance of ilxd running on localhost with the RPC
accessible to the faucet.

To run a development server.
```
faucet --dev
```

To run in production:
```
faucet --host=faucet.illium.org --tlscert=/path/to/tlscert --tlskey=/path/to/tlskey
```