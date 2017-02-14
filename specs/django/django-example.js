var Django = require("github.com/NetSys/quilt/specs/django/django")
var HaProxy = require("github.com/quilt/haproxy").Haproxy;
var Mongo = require("github.com/NetSys/quilt/specs/mongo/mongo");

// Infrastructure
var deployment = createDeployment({});

var baseMachine = new Machine({
  provider: "Amazon",
  // sshKeys: githubKeys("CHANGE_ME"), // Replace with your GitHub username
});

// Applications
var mongo = new Mongo(3);

var django = new Django({
  nWorker: 3,
  image: "quilt/django-polls",
}, mongo);

var haproxy = new HaProxy(1, django.services());

// Connections
haproxy.public();

// Deployment
deployment.deploy(baseMachine.asMaster())
deployment.deploy(baseMachine.asWorker().replicate(3))
deployment.deploy([django, mongo, haproxy]);
