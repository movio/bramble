# Example gqlgen based service with multipart file upload

This is an example service that exposes a very simple mutations:

    uploadGizmoFile(upload: Upload!) String
    uploadGadgetFile(upload: GadgetInput!): String

_Note: we have not added `gqlgen` related generated files to git; must `go generate .` before use_

To upload file you can use curl to send file to the gateway:

```
curl --request POST \
  --url http://localhost:8082/query \
  --header 'content-type: multipart/form-data' \
  --form 'operations={"query":"mutation uploadGizmoFile($upload: Upload!) {uploadGizmoFile(upload: $upload)}","variables":{"upload":null},"operationName":"uploadGizmoFile"}' \
  --form 'map={"file1": ["variables.upload"]}' \
  --form 'file1=@"sample_file.txt"'
```

With input type:

```
curl --request POST \
  --url http://localhost:8082/query \
  --header 'Content-Type: multipart/form-data' \
  --form 'operations={"query":"mutation uploadGadgetFile($upload: GadgetInput!) {uploadGadgetFile(upload: $upload)}","variables":{"upload":{"upload": null}},"operationName":"uploadGadgetFile"}' \
  --form 'map={"file1": ["variables.upload.upload"]}' \
  --form 'file1=@"sample_file.txt"'
```

Note: you **must** pass `Content-Type` headers for file upload to work. So add `Content-Type` to `allowed-headers` to `headers` plugin.
