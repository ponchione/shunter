# Hosted generation reproducibility

- Result: pass
- Command: `rtk bash scripts/static-hosted-binary-gate.sh`
- Runs: two consecutive final-state runs

Both runs passed and produced identical hashes for the contract, generated
binding, and frontend lockfile:

```text
3407936fb59f651c01349578d2448779a25c292cb409eaf0cadf898fdee10ebc  examples/hosted-chat/shunter.contract.json
0ec667f2f31326ec2b24a188efc588f96d8103aa7be1b2e7587dad8bef41d586  examples/hosted-chat/frontend/src/generated/hosted_chat.ts
b30f122bcb986752f4c37264a196ad9abc3f37736dfb64ca88ba32b6f0f5dfd4  examples/hosted-chat/frontend/package-lock.json
```

The generated binding's tracked one-line footer change aligns it with the
generator's established canonical two-newline ending. The lockfile change
records the linked `@shunter/client` dependency's exact TypeScript 5.9.3
metadata. Neither file drifted across the final gate runs.
