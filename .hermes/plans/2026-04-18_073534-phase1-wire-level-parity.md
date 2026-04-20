# Phase 1 wire-level parity plan

Verification commands
- rtk go test ./protocol -run 'TestPhase1ParityReferenceSubprotocol|TestPhase1ParityLegacyShunterSubprotocolStillAccepted|TestPhase1ParityReferenceSubprotocolPreferred' -v
