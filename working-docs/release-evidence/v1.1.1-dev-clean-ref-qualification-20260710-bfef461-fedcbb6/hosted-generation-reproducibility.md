# Hosted generation reproducibility

- Result: pass
- Command: `rtk bash scripts/static-hosted-binary-gate.sh`
- Footer: canonical two-newline ending retained
- Normalization: none performed

The hashes before and after the static hosted-binary gate were identical:

```text
3407936fb59f651c01349578d2448779a25c292cb409eaf0cadf898fdee10ebc  examples/hosted-chat/shunter.contract.json
0ec667f2f31326ec2b24a188efc588f96d8103aa7be1b2e7587dad8bef41d586  examples/hosted-chat/frontend/src/generated/hosted_chat.ts
b30f122bcb986752f4c37264a196ad9abc3f37736dfb64ca88ba32b6f0f5dfd4  examples/hosted-chat/frontend/package-lock.json
```

The generated binding ended with exactly two newline bytes both before and
after the gate. Git status for the contract, binding, and frontend lockfile was
empty, and their path-limited diff exited zero. No generated or lockfile drift
occurred.

