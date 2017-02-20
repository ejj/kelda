var HaProxy = require("github.com/quilt/haproxy").Haproxy;
var Mongo = require("github.com/quilt/mongo");
var Node = require("github.com/quilt/nodejs");
var infrastructure = require("github.com/quilt/quilt/quilt-tester/config/infrastructure")

var deployment = createDeployment({});
deployment.deploy(infrastructure);

var mongo = new Mongo(3);
var app = new Node({
  nWorker: 3,
  image: "quilt/mean-service",
  env: {
    PORT: "80",
    MONGO_URI: mongo.uri("mean-example")
  }
});
var haproxy = new HaProxy(3, app.services());

mongo.connect(mongo.port(), app);
app.connect(mongo.port(), mongo);
haproxy.public();

deployment.deploy(app);
deployment.deploy(mongo);
deployment.deploy(haproxy);
