# Deterministic simulation contract prototype

This throwaway prototype asks one narrow question for
[#64](https://github.com/gtramontina/ooze/issues/64): can arbitrary fuzz-choice bytes expand into a
legal normalized event trace that replays identically, while a separate semantic shrink removes whole
events and definition members without changing the failure fingerprint?

The answer is yes for the worked miniature campaign. `Explore` interprets bytes only against currently
enabled work and deterministically settles the remainder; `Replay` accepts the expanded typed trace;
`Shrink` removes unrelated catalogue members and whole events while preserving a typed mutant/outcome
fingerprint. The tests deliberately cross only those public prototype seams.

This is evidence for the representation split, not production code. It does not model Ooze's campaign,
runtime, or supervisor policy; choose a durable trace codec; or allocate work owned by delivery issues.
The prototype is Go rather than the usual single-file interactive logic demo because the project
requires executable contract work to follow strict red-green TDD through a Go seam and to run under
Devbox.

Run it from the repository root:

```sh
devbox run -- go -C docs/prototypes/deterministic-simulation-contract test ./...
```
