const functions = require('@google-cloud/functions-framework');

functions.cloudEvent('hello', (cloudEvent) => {
  const data = cloudEvent.data || {};
  console.log(
    `gcp-relay: received ${cloudEvent.type} ` +
      `for gs://${data.bucket}/${data.name} (size=${data.size})`,
  );
});
