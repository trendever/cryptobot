const { key, TransactionBuilder } = require('bitsharesjs');
const { Apis } = require('bitsharesjs-ws');
const rabbit = require('amqplib');


function bail(err) {
  console.error(err);
  process.exit(1);
}

const getTransaction = async (amount, to, from, privateKey) => {
  const asset_id = '1.3.861';
  const transaction = new TransactionBuilder();
  transaction.add_type_operation('transfer', {
      fee: { amount: 0, asset_id },
      amount: { amount, asset_id },
      from, to
    });

    await transaction.set_required_fees();
    await transaction.add_signer(privateKey, privateKey.toPublicKey().toPublicKeyString());
    return transaction;
}

const connectNode = async (urls) => {
  const numAttempts = 2;
  for (let i = 0; i < numAttempts; i++) {
    try { 
      await Apis.instance(urls[i], true).init_promise
      return true;
    } catch (err) {
      continue;
    }
  }
  return false;
}

const initPayer = (config) => {
  const normalizedBrainkey = key.normalize_brainKey(config.bitshares.brainkey);
  const privateKey = key.get_brainPrivateKey(normalizedBrainkey, 1);

  return async (username, amount) => {
    const connected = await connectNode(config.bitshares.nodes);
    if (!connected) {
      return { success: false, error: 'cant connect to any node'};
    }

    const precisedAmount = parseFloat(amount) * (10 ** 8)

    console.log("Transfer ", amount, ' to ', username);

    const toAccount = await Apis.instance().db_api().exec('get_account_by_name', [username]);
    const transaction = await getTransaction(precisedAmount, toAccount.id, config.bitshares.userid, privateKey)

    try {
      const result = await transaction.broadcast();
      return { success: true }
    } catch (error) {
      return { success: false, error: error.message }
    }
  }
}

const initRabbit = async (url, callback) => {
  try {
    const connection = await rabbit.connect(url);
    const channel = await connection.createChannel();
    const queueName = '__rpc__bitshares_transfer';
    await channel.assertQueue(queueName, {durable: false, autoDelete: true});
    channel.prefetch(1);
    console.log('Worker Up - Awaiting RPC requests');
    channel.consume(queueName, async (msg) => {
      const payload = JSON.parse(msg.content.toString());

      const username = payload.Name;
      const amount = payload.Amount;
      
      const result = await callback(username, amount);
      const output = JSON.stringify(result);

      channel.sendToQueue(
        msg.properties.replyTo,
        new Buffer(output),
        {correlationId: msg.properties.correlationId}
      );
      channel.ack(msg);
    });
  } catch (err) {
    bail(err)
  }
  
}

const processWork = async (config) => {
  try {
    const paymentCallback = initPayer(config);
    initRabbit(config.rabbit.URL, paymentCallback);
  } catch (err) {
    bail(err);
  }
}

module.exports = processWork;