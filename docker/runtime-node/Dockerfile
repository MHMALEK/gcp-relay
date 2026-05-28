FROM node:20-slim

# Functions Framework is NOT pre-installed; users declare it in their
# package.json (as real Cloud Functions deploys do). The entrypoint runs
# npm install at container start so the framework and user's require()
# resolve to the same module instance.

WORKDIR /workspace
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
