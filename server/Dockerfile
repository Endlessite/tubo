# Build stage
FROM maven:3.9.6-eclipse-temurin-17 AS build
WORKDIR /app
COPY pom.xml .
# Download dependencies first to cache them
RUN mvn dependency:go-offline
COPY src ./src
RUN mvn clean package -DskipTests

# Run stage
FROM eclipse-temurin:17-jre-alpine
WORKDIR /app
COPY --from=build /app/target/tubo-server-1.0.0-fat.jar ./tubo-server.jar

# Run as non-root user for better security
RUN addgroup -S tubo && adduser -S tubo -G tubo
USER tubo

ENV PORT=8080
EXPOSE 8080

ENTRYPOINT ["java", "-jar", "tubo-server.jar"]
