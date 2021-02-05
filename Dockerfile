FROM gcr.io/distroless/static

ARG ARTIFACT

ADD ${ARTIFACT} /app
ADD plugins /plugins

EXPOSE 8082

CMD [ "/app" ]
