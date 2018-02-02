FROM foreflight/base

ENV PATH=/go/bin:/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
ENV GOLANG_VERSION=1.8.3
ENV GOPATH=/go

VOLUME /etc/aws-name-server.conf

WORKDIR /go/src/app
ENTRYPOINT ["app"]
CMD ["--domain","aws.foreflight.io","--hostname","ns1.foreflight.io"]
