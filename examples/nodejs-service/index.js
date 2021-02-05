var express = require("express");
var express_graphql = require("express-graphql");
var { buildSchema } = require("graphql");
var fs = require("fs").promises;

const defaultPort = 8080;

class Node {
  constructor(id) {
    this.id = id;
  }
}

class Foo extends Node {
  constructor(id) {
    super(id);
    this.__typename = "Foo";
    this.nodejs = true;
  }
  static get(id) {
    let id = parseID(id);
    if (id.typename == "Foo") {
      return new Foo(id);
    } else {
      throw new Error(`invalid id type $`);
    }
  }
}

function parseID(id) {
  // The id format can be anything but must be global and should usually
  // contain the type and id. Here we use "type:id"
  let parts = id.split(":");
  if (parts.length == 2) {
    return {
      id: id,
      typename: parts[0],
      value: parts[1],
    };
  } else {
    throw new Error("invalid id format");
  }
}

async function setup() {
  let schemaSource = await fs.readFile("schema.graphql", "utf-8");
  let schema = buildSchema(schemaSource);

  let resolver = {
    service: {
      name: "nodejs-service",
      version: "1.0.0",
      schema: schemaSource,
    },
    foo: (args) => Foo.get(args.id),
    node: (args) => {
      let id = parseID(args.id);
      if (id.typename == "Foo") {
        return Foo.get(args.id);
      } else {
        throw new Error(`unknown type ${id.typename}`);
      }
    },
  };

  let app = express();
  app.use(
    "/",
    express_graphql({
      schema: schema,
      rootValue: resolver,
      graphiql: true,
    })
  );

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
      console.log(`example nodejs-service running on http://localhost:${port}/`)
    );
  } catch (e) {
    console.log(e);
  }
})();
