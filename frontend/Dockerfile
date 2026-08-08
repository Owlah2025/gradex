FROM node:22.22.3-alpine3.22 AS dependencies

WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM node:22.22.3-alpine3.22 AS build

WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1

COPY --from=dependencies /app/node_modules ./node_modules
COPY . .
RUN npm run build

FROM node:22.22.3-alpine3.22

ARG GRADEX_REVISION=unknown
LABEL org.opencontainers.image.revision=$GRADEX_REVISION

WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV HOSTNAME=0.0.0.0
ENV PORT=3000

COPY --from=build --chown=node:node /app/.next/standalone ./
COPY --from=build --chown=node:node /app/.next/static ./.next/static

USER node
EXPOSE 3000

CMD ["sh", "-ec", ": \"${GRADEX_API_ORIGIN:?GRADEX_API_ORIGIN is required}\"; exec node server.js"]
