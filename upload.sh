docker build --platform linux/amd64 -t sst-server:v2 .
docker save sst-server:v2 | gzip > sst-server.tar.gz
scp -i /Users/yyu/development/sst_server_key/stt-socket-root.pem /Users/yyu/GolandProjects/sst/sst-server.tar.gz root@8.219.87.116:/root/sst