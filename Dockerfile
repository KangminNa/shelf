FROM node:20-alpine

WORKDIR /app

COPY package*.json ./
COPY core/package*.json ./core/
COPY shared/package*.json ./shared/
COPY plugins/*/package*.json ./plugins/

RUN npm install --production 2>/dev/null; exit 0
RUN npm install

COPY . .

RUN npm run build 2>/dev/null; exit 0

EXPOSE 80 443 81 9100

CMD ["npx", "tsx", "core/src/index.ts"]
