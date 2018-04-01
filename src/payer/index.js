const yaml = require('js-yaml');
const fs   = require('fs');
const cluster = require('cluster');
const processWork = require('./worker');


const config = yaml.safeLoad(fs.readFileSync('/config/payer.yaml', 'utf8'));
const rabbitUrl = config.rabbit.URL;
const brainkey = config.bitshares.brainkey;


if (cluster.isMaster) {
  cluster.fork();
  cluster.on('exit', (worker) => {
    console.log('Worker ' + worker.id + ' died..');
    cluster.fork();
  });
} else {
  processWork(config);
}
