FROM gcr.io/distroless/static

ADD bramble /bramble

EXPOSE 8082
EXPOSE 8083
EXPOSE 8084

CMD [ "/bramble", "-conf", "/config.json" ]
