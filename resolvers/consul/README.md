# Design considerations

## Healthchecks 

1. The healthcheck will be created in consul under the `/v1/kv/healthchecks/<id>/<healthcheck id>` key
- The data will be json formatted with any of the valid healthcheck keys denoted in https://www.consul.io/docs/agent/checks.html. Some example keys are listed below:
```
{
  "check": {
    "id": "name",
    "script": "/bin/check_mem",
    "interval": "10s",
    "http": "http://localhost/health",
    "timeout": "5s",
    "tcp": "localhost:22",
    "ttl": "30s"
  }
}
```
An example of defining the healthcheck is shown below:
```
# cat myapp-healthcheck.json
{
  "check": {
    "id": "myapp-healthcheck",
    "http": "http://localhost:5050/health",
    "interval": "5s"
  }
}
# curl -X PUT -d @myapp-healthcheck.json http://consul.service.consul:8500/v1/kv/healthchecks/myapp/web
```

2. The marathon task will specify a label `ConsulHealthCheckKeys`. This key will have a comma separated (no spaces) list of paths to pull healthchecks from. The paths are relative to `/v1/kv/healthchecks/`.
The following marathon task definition defines three endpoints to pull healthcheck data from, `myapp/web`, `myapp/port`, and `myapp/health`.
```
# cat myapp.json
{
  "id": "myapp",
  ...
  "labels:" {
    "ConsulHealthCheckKeys": "myapp/web,myapp/port,myapp/health"
  }
}
```

