var spark = require("github.com/quilt/spark");
var infrastructure = require("github.com/quilt/quilt/quilt-tester/config/infrastructure");

// Application
// sprk.exclusive enforces that no two Spark containers should be on the
// same node. sprk.public says that the containers should be allowed to talk
// on the public internet. sprk.job causes Spark to run that job when it
// boots.
var sprk = new spark.Spark(1, 3)
    .exclusive()
    .public()
    .job("run-example SparkPi");

var deployment = createDeployment({})
deployment.deploy(infrastructure)
deployment.deploy(sprk);

deployment.assert(publicInternet.canReach(sprk.masters), true);
deployment.assert(enough, true);
