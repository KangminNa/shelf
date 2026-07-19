const http = require('http');
http.createServer((req, res) => {
  res.end('hello from docker app v1');
}).listen(3000, () => console.log('hello-app listening on 3000'));
