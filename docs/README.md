# CloudNativePG documentation

The documentation is compiled using [MkDocs](https://www.mkdocs.org/)

Run the following command to build the documentation
in the `dist` directory:

``` bash
docker run --rm -v "$(pwd):$(pwd)" -w "$(pwd)" \
    quay.io/enterprisedb/cloud-native-docutils:mkdocs \
    mkdocs build -v -d dist
```

You can locally test the documentation by executing
the following command and pointing your browser
to http://127.0.0.1:8000/

``` bash
docker run --rm -v "$(pwd):$(pwd)" -w "$(pwd)" -p 8000:8000 \
    quay.io/enterprisedb/cloud-native-docutils:mkdocs \
    mkdocs serve -a 0.0.0.0:8000
```
