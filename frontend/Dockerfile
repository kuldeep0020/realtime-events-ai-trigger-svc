FROM node:20-alpine

WORKDIR /app

RUN corepack enable && corepack prepare pnpm@9.15.9 --activate

COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

COPY . .

ARG NEXT_PUBLIC_API_BASE=https://realtime-events-ai-trigger-api.dev-rudder.rudderlabs.com
ENV NEXT_PUBLIC_API_BASE=$NEXT_PUBLIC_API_BASE

RUN NODE_ENV=production pnpm build

EXPOSE 3001

ENV NODE_ENV=production

CMD ["pnpm", "start"]
