var express = require("express");
var { graphqlHTTP } = require('express-graphql');
var { buildSchema } = require("graphql");
var fs = require("fs").promises;

const defaultPort = 8080;
class Gizmo {
  constructor(id) {
    this.id = id;
    this.rating = Math.floor(Math.random() * 100);
  }
  static get(id) {
    return new Gizmo(id);
  }
}

async function setup() {
  let schemaSource = await fs.readFile("schema.graphql", "utf-8");
  let schema = buildSchema(schemaSource);

  let resolver = {
    service: {
      name: "nodejs-service",
      version: "0.1.0",
      schema: schemaSource,
    },
    gizmo: (args) => Gizmo.get(args.id),
  };

  let app = express();
  app.use(
    "/query",
    graphqlHTTP({
      schema: schema,
      rootValue: resolver,
      graphiql: true,
    })
  );

  app.use('/health', (req, res) => {
    res.send('OK')
  });

  return app;
}

(async () => {
  try {
    let app = await setup();
    let port = process.env.PORT;
    if (port === undefined) {
      port = defaultPort;
    }
    app.listen(port, () =>
      console.log(`example nodejs-service running on :${port}`)
    );
  } catch (e) {
    console.log(e);
  }
})();
