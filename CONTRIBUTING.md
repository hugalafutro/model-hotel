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
- Run `make test` before submitting.
- Be excellent to each other.

## Running Tests

`make test` runs the full suite. Some tests in `internal/util` expect a
running Docker daemon (the project is designed for Docker-first deployment).
These tests pass regardless of whether Docker is available, but they only
contribute full coverage when the Docker socket is reachable.

That's it. Thanks for helping make Model Hotel better!