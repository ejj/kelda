const spark = require('@kelda/spark');
const infrastructure = require('../../config/infrastructure.js');

const infra = infrastructure.createTestInfrastructure();

const sprk = new spark.Spark(3);
const sprk2 = new spark.Spark(3);

sprk.deploy(infra);
sprk2.deploy(infra);
