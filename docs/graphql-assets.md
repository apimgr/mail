# GraphiQL browser assets

The GraphQL documentation page serves these pinned assets locally and does not
load executable code from a CDN.

| Asset | Upstream version | Source | License | SHA-256 |
|---|---:|---|---|---|
| `react-16.13.1.min.js` | React 16.13.1 | https://unpkg.com/react@16.13.1/umd/react.production.min.js | MIT (`REACT-LICENSE`) | `c9486f126615859fc61ac84840a02b2efc920d287a71d99d708c74b2947750fe` |
| `react-dom-16.13.1.min.js` | ReactDOM 16.13.1 | https://unpkg.com/react-dom@16.13.1/umd/react-dom.production.min.js | MIT (`REACT-DOM-LICENSE`) | `bc5b7797e8a595e365c1385b0d47683d3a85f3533c58d499659b771c48ec6d25` |
| `graphiql-1.4.7.min.js` | GraphiQL 1.4.7 | https://unpkg.com/graphiql@1.4.7/graphiql.min.js | MIT (`GRAPHIQL-LICENSE`) | `72967cc3d0ff8ba5dd11b63f12eef2017798cd1795c349b161dece53781472c7` |
| `graphiql-1.4.7.min.css` | GraphiQL 1.4.7 | https://unpkg.com/graphiql@1.4.7/graphiql.min.css | MIT (`GRAPHIQL-LICENSE`) | `1c00d0a3052e16e9b4dbe0a492fe58bb9ca04682e594766a8344c565763b3472` |

GraphiQL 1.4.7 includes the upstream fix for the XSS issue affecting earlier
releases. The exact license texts are kept beside the assets under
`src/server/web/static/vendor/graphiql/`. Recompute checksums when upgrading
any asset.
