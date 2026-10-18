# Build from the repository root, because the relay serves ../run.sh and ../install.sh:
#   docker build -f server/Dockerfile .
FROM maven:3.9-eclipse-temurin-17 AS build
WORKDIR /app/server
COPY server/pom.xml .
RUN mvn -B -q dependency:go-offline
COPY server/src ./src
COPY run.sh install.sh /app/
RUN mvn -B -q package -DskipTests

FROM eclipse-temurin:17-jre-alpine
WORKDIR /app
COPY --from=build /app/server/target/tubo-server-1.0.0-fat.jar ./tubo-server.jar
RUN addgroup -S tubo && adduser -S tubo -G tubo
USER tubo
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["java", "-jar", "tubo-server.jar"]
