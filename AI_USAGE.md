# AI Usage

## Tools used

- GitHub Copilot using VS Code Agent Mode, primarily for workspace inspection,
  code implementation, test creation, documentation, and verification planning.

## Rough usage

- Read the assignment and starter/debugging files.
- Designed item-level batch validation and in-memory de-duplication.
- Implemented the Go HTTP handlers, tests, debugging fixes, and submission notes.
- Organized verification commands and expected behavior.

## Rejected or changed suggestion

AI initially suggested strict unknown-field rejection for every provider record.
I changed that approach: unknown fields are permitted because the fixed contract
does not say providers must never add fields. Validation rejects missing required
fields and bad types/timestamps, while forward-compatible fields remain accepted.

## One thing AI helped me understand

It clarified that campaign raw `opened` counts and contact-based `unique_opens`
are separate metrics and should not share the same deduplication rule.

## Generated code I debugged

- The initial batch handler response logic was tightened so mixed batches use
  207 Multi-Status and reject-only batches use 400.
- A file decoding detail was corrected to use `bytes.NewReader`; this prevents
  decoding each raw array item through an invalid helper assumption.
