(function () {
  "use strict";

  var root = document.getElementById("graphiql");
  if (!root || !window.React || !window.ReactDOM || !window.GraphiQL) return;

  var endpoint = root.dataset.endpoint;
  var fetcher = function (params, options) {
    var headers = Object.assign(
      { Accept: "application/json", "Content-Type": "application/json" },
      (options && options.headers) || {}
    );
    return fetch(endpoint, {
      method: "POST",
      headers: headers,
      body: JSON.stringify(params),
      credentials: "same-origin"
    }).then(function (response) {
      return response.json();
    });
  };

  ReactDOM.render(
    React.createElement(GraphiQL, {
      fetcher: fetcher,
      headerEditorEnabled: true,
      shouldPersistHeaders: false,
      query: "query MailServices { mailServices { name installed status running version } }"
    }),
    root
  );
})();
