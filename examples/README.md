# Examples

Deployment examples. Each directory has its own `config.yaml`; `config.example.yaml` in the repo root documents every key. The messager needs no tools and no database: its only state is in the target channel, which it reads on start. Create its Slack app first (README, steps 1 to 4) and run itakeit in the channel from its own repository ([itakeit's examples](https://github.com/nice-pink/itakeit/tree/main/examples) run it with Compose or in Kubernetes). Run one messager per Slack app: Slack delivers each Socket Mode event to one open connection, so a second instance takes events away from the first.

| Directory | Runs | Claude login |
|---|---|---|
| `docker-compose/` | locally, built from this checkout | `CLAUDE_CODE_OAUTH_TOKEN` in `.env` |
| `kubernetes/` | Kubernetes, published image | `CLAUDE_CODE_OAUTH_TOKEN` in a Secret |

Both use `backend: claude-code` with a token from `claude setup-token`, which bills the plan of that login. For `backend: api`, set it in `config.yaml` and pass `ANTHROPIC_API_KEY` instead of the OAuth token.

## Docker Compose

```
cd examples/docker-compose && cp .env.example .env && docker compose up --build
```

Put the tokens in `.env` (gitignored) and `target_channel` in `config.yaml`. The container runs read-only with tmpfs at `/tmp` and `/home/node`, where the Claude CLI writes its state on every call. `docker compose logs -f` shows `authenticated`, then `model ready` after the probe call, then `recovered posted tasks`.

A quick test without Slack events is the stdin source: with `stdin: true` and `slack.enabled: false` in `config.yaml`, pipe a line in and watch it get classified. A task is posted to the target channel, so point it at a test channel:

```
echo "The nightly export has failed three times, can someone look?" | docker compose run --rm -T messager
```

A stdin line's key is its position and text, so once a line posted a task, a repeat of the same line is dropped as a duplicate on the next run: change the text to test again.

## Kubernetes

A kustomize directory in namespace `itakeit`, the namespace itakeit's examples use, running one replica with the `Recreate` strategy for the reason above. Tokens never go in the files: apply first, which creates the namespace, then create the Secret. The pod waits in `CreateContainerConfigError` until it exists. Both commands are safe to re-run. `kubectl delete -k` deletes the namespace, and with it itakeit if it runs there.

```
kubectl apply -k examples/kubernetes
```

```
kubectl -n itakeit create secret generic itakeit-messager --from-literal=MESSAGER_SLACK_BOT_TOKEN=xoxb-... --from-literal=MESSAGER_SLACK_APP_TOKEN=xapp-... --from-literal=CLAUDE_CODE_OAUTH_TOKEN=sk-ant-... --dry-run=client -o yaml | kubectl apply -f -
```

There is no readiness endpoint. Check it with `kubectl -n itakeit logs deploy/itakeit-messager`, which logs `authenticated`, `model ready` and `recovered posted tasks` once up. The pod runs under the "restricted" Pod Security level. Pin `image:` to a release tag (`X.Y.Z`) or `sha-<short>` rather than `latest` for anything you depend on. Editing `config.yaml` and re-applying rolls the pod, since the ConfigMap name carries a content hash.

### HTTP source

To take messages from other systems over HTTP inside the cluster, set in `config.yaml`:

```yaml
  http:
    enabled: true
    listen: 0.0.0.0:8080
```

add `MESSAGER_HTTP_TOKEN` (16+ characters) to the Secret, and add `http.yaml` to `kustomization.yaml`. It defines a ClusterIP Service, so pods in the cluster reach `http://itakeit-messager.itakeit:8080/messages`. Put an Ingress with TLS in front before exposing it further.
