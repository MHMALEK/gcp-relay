FROM python:3.12-slim

# Flush stdout/stderr so the user's print() output appears in `docker logs`
# without waiting for buffer flushes.
ENV PYTHONUNBUFFERED=1

RUN pip install --no-cache-dir "functions-framework==3.*"

WORKDIR /workspace
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
