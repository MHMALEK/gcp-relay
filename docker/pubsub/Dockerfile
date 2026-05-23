FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    gnupg \
    openjdk-17-jre-headless \
  && curl -fsSL https://packages.cloud.google.com/apt/doc/apt-key.gpg \
    | gpg --dearmor -o /usr/share/keyrings/cloud.google.gpg \
  && echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" \
    > /etc/apt/sources.list.d/google-cloud-sdk.list \
  && apt-get update \
  && apt-get install -y --no-install-recommends google-cloud-cli google-cloud-cli-pubsub-emulator \
  && rm -rf /var/lib/apt/lists/*

EXPOSE 8085
CMD ["gcloud", "beta", "emulators", "pubsub", "start", "--host-port=0.0.0.0:8085", "--project=local-project"]
