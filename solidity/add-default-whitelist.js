const networks = {
  staging: 'https://ganache.staging.dev.tor.us',
};

module.exports = async function(callback) {
  var whitelistedAccounts = [
    '0x52c476751142ce2fb4db4f19b500e78feee10b06',
    '0xff364b6b86ea5a4f59cc4989da23b833dac15304',
    '0xdc0dd04aac998e8aa9f2de236b3ba04ddafd26ca',
    '0x253db77f1ae216722b2f67f33ef3c8e00b2689e6',
    '0x271346169993368f94cb2c443b8b8cdbdd5edf04',
    '0xa0ae28ec27fea7a577b21330f6ce8ae45a55fe76',
    '0xf34a875cffe643d44546b76f0c9412dfb9d2b379',
    '0x35d946c9c4598cd2eaee5754ce2041911dc816ce',
    '0xd6ee5e06ac11a62fd0be1912debeeb4abc24f723',
    '0x40fa4b9e4411e7f5f58713eff426cad4f0294ab5',
    '0x0cda757357158e4d8ad94433e36f1fe05a1dc576',
    '0xa22e3c16264dc688107142776139d1fb4bb9d549',
    '0x0b998b7229bfd254acf50b4e2739e73d937dc1c9',
    '0xfc54c26e24b4570590c11486bd627aa4b7339523',
    '0xb572081928b988abe713ffe60f8cf28ef80eee07',
    '0xd54e0c310a97916e67d07aa501f74524e82c3af1',
    '0xaba31e255b490365584a56f4ebc5037963e584d5',
    '0x3ecefafea7db9d0e26dc0d266504587cb66f6008',
    '0x184b56d50300b4cd604a587491cb7bcb0ffc7454',
    '0xd6eca392ada22e18c9eebde2828b38e66813af5f',
  ];
  const Web3 = require('web3');
  const truffleContract = require('truffle-contract');
  let contract = truffleContract(require('./build/contracts/NodeList.json'));
  var provider = new Web3.providers.HttpProvider(networks.staging);
  var web3 = new Web3(provider);
  contract.setProvider(web3.currentProvider);

  //workaround: https://github.com/trufflesuite/truffle-contract/issues/57
  if (typeof contract.currentProvider.sendAsync !== 'function') {
    contract.currentProvider.sendAsync = function() {
      return contract.currentProvider.send.apply(contract.currentProvider, arguments);
    };
  }

  contract
    .deployed()
    .then(async instance => {
      whitelistedAccounts.forEach(async acc => {
        await instance.updateWhiteList(0, acc, true);
      });
    })
    .then(response => {
      console.log(response);
    });
};
