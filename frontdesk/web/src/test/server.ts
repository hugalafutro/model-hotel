import { setupServer } from "msw/node";

// Shared MSW server. Individual tests add the handlers they need with
// server.use(...); there are no default handlers so each test declares its own
// expected surface explicitly.
export const server = setupServer();
