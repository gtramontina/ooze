# Static non-re-entry for StartCommitted installation

## Question

Can Go make it impossible for the private `StartCommitted` installer to call back into the
process runtime while the runtime mutex is held, in the same spirit as a typestate API?

## Conclusion

Partly. Narrowing the callback parameter to a smaller interface does **not** solve the problem.
Go function literals are closures and may refer to variables in their surrounding function, so an
installer with an apparently harmless signature can still capture `*processRuntimeShell` and call it.
Function types describe parameters and results, not captured capabilities. See the Go specification's
[function type](https://go.dev/ref/spec#Function_types) and
[function literal](https://go.dev/ref/spec#Function_literals) rules.

The callback can instead be removed. A private concrete, data-only two-stage value can make the
dangerous call path absent:

```go
type startInstallation struct {
	grant  admissionGrant // inert exact authority identity
	target *pendingStartCell
}

type installedStart struct {
	generation attemptGeneration
	target     *pendingStartCell
	fatal      *processRuntimeShell
}

func (i startInstallation) install(generation attemptGeneration, fatal *processRuntimeShell) installedStart {
	// Broker-owned code only: validate an empty target, then perform a direct
	// state transition. No caller-supplied executable value exists here.
	i.target.install(generation)

	return installedStart{generation: generation, target: i.target, fatal: fatal}
}

func (s installedStart) launch(run func(attemptGeneration) attemptObservation) attemptObservation {
	if !s.target.claimLaunch(s.generation) { // rejects zero, mismatch, and reuse
		s.fatal.failLaunchInvariant() // transfers residual custody, then panics
	}
	return run(s.generation)
}
```

The runtime accepts `startInstallation`, not `func(generation) installedStart`. While holding its
mutex it first validates that the request, delivered grant, and installation authority are identical.
Only then does it derive the candidate prospective obligation, perform the broker-owned cell
installation, and only then publishes the candidate state before unlock. If installation rejects a zero, nil, or
reused value, the candidate is never published, so emergency closure cannot observe an unmatched
runtime obligation. A cross-paired authority fails from the pre-commit state with neither cell changed.
No executable value is an input to that locked operation. Only after it returns may the caller supply
a native thunk to `installedStart.launch`, which receives the exact installed generation. That closure
may capture the broker, but it cannot exist on the locked call path. The post-unlock capability retains a concrete private
fatal guard so validation failure cannot be ignored: it atomically closes the runtime, transfers its
correlated residual custody, and panics. Neither `startInstallation` nor the locked method input gains
a function, interface, or executable callback.

That guard also closes the shell's single process-wide emergency channel before re-panicking. The
channel is created once with the shell and exposed receive-only; all registered campaign drivers
observe the same close. This is distinct from the per-request buffered one-shots: it lets a campaign
with no current waiter enter `RuntimeEmergency`, and lets peers holding already-delivered grants
acknowledge the return authorities retained by fatal closure. The runtime mutex serializes the sole
`close`, so later causes only join the fatal epoch and cannot close or replace the channel again.
Each attempt-originated fatal seed carries its exact generation in the stable ingress-ordered cause
ledger. Equal cause kinds from different attempts therefore remain distinct; a repeated observation
for the same generation is an invariant violation rather than accidental idempotence.
Post-unlock launch invariants use the installed cell generation as their cause identity. Their guard
closes and settles under the shell lock, returns through the guard so it records that ingress once,
then re-panics only after unlock; the generic panic guard cannot mistake the deliberate re-panic for
a second fatal ingress.
Once emergency settlement transfers a prospective obligation, a later no-release observation cannot
delete or reclassify that custody. Only an unresolved prospective (`None` or `FatalSeeded`) may still
resolve as proven not released; transferred or settled custody remains in the stable residual.
When an unresolved prospective is later proven not released, that correlated settlement also derives
the fatal final cut; it cannot leave an empty runtime stranded in `FatalClosing`.

This is typestate-like: `startInstallation` represents “not installed” and `installedStart`
represents “installed and launchable.” Their distinct method sets determine which operations are
available, as specified by Go's [method-set rules](https://go.dev/ref/spec#Method_sets). The safety
comes from combining those types with a stronger property: the locked phase contains no
caller-supplied executable behavior.

## Callback-seam retraction

The earlier #62 prototype used a private `generationInstaller func(attemptGeneration)
installedLaunch`. That seam is retracted. Its private visibility and narrow signature did not make
re-entry invalid: a closure could capture the broker and deadlock by calling it while the runtime
mutex was held. The replacement is the concrete `startInstallation -> installedStart` transition
above. This is a contract correction driven by a counterexample, not an additional public seam or a
second reducer.

## Why narrower interfaces are insufficient

Consider an installer that receives only a narrow capability:

```go
type generationSink interface {
	install(attemptGeneration)
}

func installer(sink generationSink) installedStart {
	broker.snapshot() // captured from the surrounding scope; still type-checks
	// ...
}
```

The interface limits calls through `sink`, but it does not limit names captured by the closure.
Interfaces define method sets and type sets; they do not describe effects or closure environments
([Go specification](https://go.dev/ref/spec#Interface_types)). Generics have the same limitation:
constraints control operations on a type parameter, not what other values a function body may use.

An unexported method can seal an interface against implementations from other packages, because
unexported identifiers are package-scoped and package-qualified
([export rules](https://go.dev/ref/spec#Exported_identifiers)). It still does not stop code in the
same package from capturing the broker, and #62 deliberately keeps runtime and orchestration inputs
in one package.

## What Go typestate can and cannot guarantee

Distinct concrete types can statically remove methods from a phase. They cannot provide Rust-like
linear ownership:

- Go values and pointers can be copied or aliased. A `startInstallation` or `installedStart` value
  can be copied and presented twice unless the shared cell rejects the second installation or
  launch at runtime.
- A function value's type does not reveal whether it captured the broker.
- A `sync.Mutex` is not associated with a goroutine. Locking an already-held mutex blocks, and the
  library cannot identify “the same goroutine” as a special case
  ([`sync.Mutex` documentation](https://pkg.go.dev/sync#Mutex)).

Rust closures, by contrast, record capture modes and participate in borrowing, and `FnOnce` may
consume a closure. Those are language properties documented in the
[Rust closure reference](https://doc.rust-lang.org/reference/types/closure.html). Go has no
equivalent borrow checker or affine/linear function kind.

Therefore the data-only design still needs runtime assertions for one-shot installation, exact
generation, nil target, and a nil thunk supplied to `installedStart`. Those checks guard copying
and malformed values; their failure closes custody and panics rather than returning an error that a
caller could ignore. They do not guard re-entry, because no foreign code exists during installation.

## Stronger package boundary

If the future supervisor lives in a separate internal package, the compiler can strengthen the
boundary further:

1. the supervisor package exposes a concrete installation value rather than an interface or
   callback;
2. its locked installation path contains no function/interface fields;
3. the supervisor package does not import the broker package, enforced by Go's package dependency
   graph and import-cycle prohibition;
4. the native thunk is supplied only after the broker unlocks.

That package split is optional and may conflict with #62's settled shared-package decision. The
same-package concrete value already removes accidental closure capture from the call site; a package
split additionally prevents the trusted installation implementation from naming the broker.

## Static analysis as a guardrail

Go's standard `copylocks` analyzer checks lock values passed by value; it does not detect lock
re-entry ([analyzer documentation](https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/copylock)).
A repository-specific analyzer could forbid function/interface fields on the installation type and
forbid broker calls from its locked transition. The official
[`go/analysis` API](https://pkg.go.dev/golang.org/x/tools/go/analysis) provides syntax, type, control
flow, and SSA inputs for such checks. This is useful defense in depth, but the concrete data-only API
should carry the primary design guarantee.

## Recommendation for #62

Replace `generationInstaller func(...)` with an unexported concrete two-stage installation value:

1. `startInstallation` carries only inert exact grant identity and a pointer to the future
   supervisor's private pending-start cell; it contains no function or interface value.
2. Broker-owned `install` performs the exact generation transition under the runtime lock without
   invoking caller code.
3. It returns `installedStart`; only that narrower value accepts the native thunk after unlock and
   supplies that thunk the exact installed generation.
4. The pending cell rejects duplicate/wrong-generation installation and launch reuse at runtime;
   the post-unlock fatal guard transfers residual custody and panics, compensating for Go's copyable
   values without an ignorable failure return.
5. #67 embeds or owns the pending cell and supplies the post-unlock native thunk; #62 need not add
   platform supervision.

This preserves the atomic install-before-unlock cut, removes the re-entry deadlock by construction,
and does not require goroutine identity, `TryLock`, a serializer goroutine, or a public interface.
