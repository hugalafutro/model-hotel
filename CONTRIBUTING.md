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

Run `make setup` once after cloning. It points `core.hooksPath` at `scripts/`,
which is what enables the hooks described below; without it they never run.

The backend tests use their own Postgres, which is **not** the dev stack's:

```bash
make test-db-up       # test Postgres on :5433 (docker-compose.test.yml)
make test             # backend tests: go test ./...
make test-parallel    # ./internal/... only, sharded across processes; faster
make lint             # golangci-lint
make size-check       # file-size ratchet: 800 lines production, 2000 test
```

`make test-parallel` is the one to reach for while iterating, but it shards
`./internal/...` alone; `make test` is what covers `cmd/` and `tools/` as well,
so run it before pushing.

The size ratchet decides what counts as a test by path, not by filename: a Go
`_test.go` file gets the 2000-line ceiling, and so does anything under a
`__tests__/` directory or in `web/src/test/` or `frontdesk/web/src/test/`. A
`.test.ts` or `.spec.ts` anywhere else is treated as production code and gets
the 800-line ceiling, because a production file can be given that suffix too.

`make docker-up` starts the dev stack for running the app. Its Postgres
publishes no port, so it is not what the tests connect to; use `make test-db-up`
for those.

The frontend (in `web/`) has its own suite, linter, and type-check. Front Desk
(`frontdesk/web/`) is a second SPA with its own suite and locales, so run the
same commands there when you touch it:

```bash
cd web
pnpm install
pnpm vitest run --coverage   # tests + coverage
pnpm run lint                # eslint
pnpm exec tsc -b             # type-check (stricter than the editor; run it)
```

CI enforces a **90% coverage threshold** (backend, frontend, and Front Desk
web), a separate **90% diff-coverage gate** on the lines your PR changes, the
file-size ratchet above, and **locale parity** via `make i18n-check` (fully
offline). If you add a user-facing string, add it to `en.json` and translate it
into the other locales by hand. Intentional English goes in the allowlist for
that app: `tools/i18n-translate/allow-english.json` for the dashboard,
`allow-english-fd.json` for Front Desk, `allow-english-android.json` for
Bellhop.

If your change touches `README.md`, mirror it in `DOCKERHUB.md`, since the two
describe the same project to different audiences. When the change genuinely
does not belong on the Docker Hub page, note that clearing this takes two
separate steps: `DOCKERHUB_README_NOT_NEEDED=1` lets the local pre-push hook
through, while CI reads the `dockerhub-readme-not-needed` label on the PR. Skip
the label and the `README/DOCKERHUB.md sync` check fails even though your push
succeeded.

The hooks under `scripts/` are a fast pre-flight, not a substitute for CI. On
push they run golangci-lint, `pnpm lint`, `tsc -b`, the vitest tests related to
your changes, and the diff-coverage gate; the authoritative gate is still the
full CI run on GitHub.

Some tests in `internal/util` expect a running Docker daemon (the project is
designed for Docker-first deployment). They pass whether or not Docker is
available, but only contribute full coverage when the Docker socket is reachable.

That's it. Thanks for helping make Model Hotel better!
