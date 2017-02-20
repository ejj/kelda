// Place a google/pause container on each worker machine.

var infrastructure = require("github.com/quilt/quilt/quilt-tester/config/infrastructure")

var deployment = createDeployment({});
deployment.deploy(infrastructure);

var nWorker = deployment.machines.filter(function(m) {
    return m.role == "Worker"
}).length;
var containers = new Service("containers", new Container("google/pause").replicate(nWorker));
containers.place(new LabelRule(true, containers));

deployment.deploy(containers);
