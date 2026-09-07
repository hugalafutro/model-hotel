package util

// SecretMask is the placeholder a GET returns in place of a stored secret and
// that a PUT echoes back to mean "keep what is stored". The dashboard SPA, the
// Front Desk SPA and Bellhop all compare against this exact value, so it is one
// constant rather than a convention repeated per package.
const SecretMask = "********"
