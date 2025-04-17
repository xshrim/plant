FROM alpine:latest

RUN apk update && apk upgrade && apk --virtual add --no-cache bash curl util-linux tzdata bind-tools busybox-extras jq yq vim chromium && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime

ADD plant /root/

WORKDIR /root

EXPOSE 2333

CMD ["./plant"]