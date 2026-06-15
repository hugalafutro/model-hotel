# Contributing to Model Hotel

Thank you for considering a contribution! This project is released under the MIT License, and we want to keep things simple and open.

## License Agreement

By submitting a pull request or otherwise contributing code, documentation, or other materials to this repository ("Contributions"), you agree to the following:

1. **Your Contributions are licensed under the MIT License.**
   You grant everyone permission to use, copy, modify, merge, publish, distribute,
   sublicense, and/or sell your Contributions under the terms of the MIT License.

2. **Additional license grant to the maintainers.**
   You also grant the project maintainers a perpetual, worldwide, non-exclusive,
   royalty-free, irrevocable license to use, reproduce, modify, adapt, publish,
   translate, create derivative works from, distribute, perform, display, and
   sublicense your Contributions. This includes the right to relicense your
   Contributions under different terms in future versions of the software.

3. **You retain ownership of your Contributions.**
   This agreement does not transfer copyright to the project. You keep the rights
   to use your own work in other projects.

4. **Original work only.**
   You represent that each of your Contributions is your original creation and
   that you have the legal right to grant the above licenses. If you include
   third-party code, clearly mark it and provide its license terms.

## Practical Stuff

- Open an issue to discuss large changes before investing time in a PR.
- Keep commits focused and write clear commit messages.
- Run the checks below before submitting — CI enforces all of them, so running
  them locally first saves a round-trip.
- Be excellent to each other.

## Building & Testing

The backend tests need a Postgres instance, so start the dev stack first:

```bash
make docker-up        # start Postgres (+ the dev stack)
make test             # backend tests: go test ./...
make lint             # golangci-lint
```

The frontend (in `web/`) has its own suite, linter, and type-check:

```bash
cd web
pnpm install
pnpm vitest run --coverage   # tests + coverage
pnpm run lint                # eslint
pnpm exec tsc -b             # type-check (stricter than the editor; run it)
```

CI additionally enforces an **80% coverage threshold** (backend and frontend)
and **locale parity** via `make i18n-check`. If you add a user-facing string,
run `make i18n-fill` to populate the other locales (or add intentional English
to the allowlist) so the check passes.

The repo ships git hooks under `scripts/` (enabled via `core.hooksPath`); on
push they run go vet, the linters, and `tsc -b` as a fast pre-flight, but the
authoritative gate is the full CI run on GitHub.

Some tests in `internal/util` expect a running Docker daemon (the project is
designed for Docker-first deployment). They pass whether or not Docker is
available, but only contribute full coverage when the Docker socket is reachable.

That's it. Thanks for helping make Model Hotel better!
