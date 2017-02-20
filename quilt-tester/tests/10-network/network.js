var infrastructure = require("github.com/quilt/quilt/quilt-tester/config/infrastructure")

var deployment = createDeployment({});
deployment.deploy(infrastructure);

var c = new Container("alpine", ["tail", "-f", "/dev/null"]);
var red = new Service("red", c.replicate(5));
var blue = new Service("blue", c.replicate(5));
var yellow = new Service("yellow", c.replicate(5));

red.connect(80, blue);
blue.connect(80, red);
red.connect(80, yellow);
blue.connect(80, yellow);

deployment.deploy(red);
deployment.deploy(blue);
deployment.deploy(yellow);
