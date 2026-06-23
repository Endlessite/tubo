// Tubo relay server — stateless encrypted file streaming over WebSocket + HTTP.
// See README.md for protocol details.
package com.endlessite.server;

import io.vertx.core.Future;
import io.vertx.core.VerticleBase;
import io.vertx.core.Vertx;
import io.vertx.core.http.HttpServerResponse;
import io.vertx.core.http.ServerWebSocket;
import io.vertx.core.json.JsonObject;
import io.vertx.ext.web.Router;
import io.vertx.ext.web.RoutingContext;

import java.security.MessageDigest;
import java.security.SecureRandom;
import java.util.Base64;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

public class MainVerticle extends VerticleBase {

  private static final String CHARSET = "abcdefghijklmnopqrstuvwxyz0123456789";
  private static final int MAX_ACTIVE_SESSIONS = 10000;
  private static final SecureRandom RANDOM = new SecureRandom();
  private static final long SESSION_TIMEOUT_MS = 10 * 60 * 1000;
  
  private static final String ASCII_LOGO = 
    "  _____       _             \n" +
    " |_   _|   _ | |__   ___  \n" +
    "   | | | | | | '_ \\ / _ \\ \n" +
    "   | | |_| | | |_) | (_) |\n" +
    "   |_|\\__,_| |_.__/ \\___/ \n" +
    "                          \n" +
    "Tubo - Zero-install End-to-End Encrypted File Transfer\n" +
    "======================================================\n\n";


  private static class SessionData {
    public final String id;
    public final String password;
    public String fileName = "download.dat";
    public boolean senderMetaReceived = false;

    public ServerWebSocket senderWs;
    public ServerWebSocket receiverWs;
    public HttpServerResponse receiverRes; // Set when the receiver is an HTTP GET (curl/script)
    public RoutingContext senderCtx;       // Set when the sender is an HTTP POST (curl/script)
    public Runnable onStartCallback;       // Resumes paused HTTP POST stream on receiver "start"
    public long timerId = -1;              // Inactivity timeout timer, cancelled when counterpart joins

    public SessionData(String id, String password) {
      this.id = id;
      this.password = password;
    }
  }


  private final ConcurrentHashMap<String, SessionData> activeSessions = new ConcurrentHashMap<>();

  public static void main(String[] args) {
    Vertx vertx = Vertx.vertx();
    vertx.deployVerticle(new MainVerticle());

    // Graceful shutdown: wait for active transfers
    Runtime.getRuntime().addShutdownHook(new Thread(() -> {
      System.out.println("Received shutdown signal. Waiting for active transfers...");
      CountDownLatch latch = new CountDownLatch(1);
      vertx.close().onComplete(ar -> latch.countDown());
      try {
        latch.await(30, TimeUnit.SECONDS);
      } catch (InterruptedException ignored) {}
    }));
  }


  private String generateRandomString(int length) {
    StringBuilder sb = new StringBuilder(length);
    for (int i = 0; i < length; i++) {
      sb.append(CHARSET.charAt(RANDOM.nextInt(CHARSET.length())));
    }
    return sb.toString();
  }

  // Pipe to receiver with backpressure
  private void forwardBinary(SessionData session, ServerWebSocket senderWs, io.vertx.core.buffer.Buffer buffer) {
    if (session.receiverWs != null) {
      session.receiverWs.writeBinaryMessage(buffer);
      if (session.receiverWs.writeQueueFull()) {
        senderWs.pause();
        session.receiverWs.drainHandler(v -> senderWs.resume());
      }
    } else if (session.receiverRes != null) {
      session.receiverRes.write(buffer);
      if (session.receiverRes.writeQueueFull()) {
        senderWs.pause();
        session.receiverRes.drainHandler(v -> senderWs.resume());
      }
    }
  }


  private void forwardText(SessionData session, String msg) {
    if (session.receiverWs != null) {
      session.receiverWs.writeTextMessage(msg);
    }
  }


  private void setupSenderHandlers(SessionData session, ServerWebSocket webSocket) {
    session.senderWs = webSocket;
    webSocket.binaryMessageHandler(buffer -> forwardBinary(session, webSocket, buffer));
    webSocket.textMessageHandler(msg -> {
      try {
        JsonObject json = new JsonObject(msg);
        if ("meta".equals(json.getString("type"))) {
          String fname = json.getString("filename");
          if (fname != null) session.fileName = fname.replaceAll("[\"\\r\\n]", "_");
          session.senderMetaReceived = true;
          if (session.receiverWs != null) {
            session.receiverWs.writeTextMessage(
              new JsonObject().put("status", "ready").put("filename", session.fileName).encode());
          }
        }
      } catch (Exception ignored) {}
      forwardText(session, msg);
    });
  }


  private void setupReceiverHandlers(SessionData session, ServerWebSocket webSocket) {
    session.receiverWs = webSocket;
    webSocket.textMessageHandler(msg -> {
      try {
        JsonObject json = new JsonObject(msg);
        if ("start".equals(json.getString("action"))) {
          if (session.senderWs != null) {
            session.senderWs.writeTextMessage(new JsonObject().put("status", "start").encode());
          }
          if (session.onStartCallback != null) {
            session.onStartCallback.run();
          }
        }
      } catch (Exception e) {
        System.err.println("Invalid receiver message: " + e.getMessage());
      }
    });
  }


  private void destroySession(SessionData session, ServerWebSocket self, String sessionId) {
    if (session.senderWs != null && (self == null || session.senderWs != self)) session.senderWs.close();
    if (session.receiverWs != null && (self == null || session.receiverWs != self)) session.receiverWs.close();
    if (session.receiverRes != null) session.receiverRes.end();
    if (session.senderCtx != null) session.senderCtx.request().connection().close();
    if (session.timerId != -1) vertx.cancelTimer(session.timerId);
    activeSessions.remove(sessionId);
  }

  @Override
  public Future<?> start() {
    Router router = Router.router(vertx);

    // --- WebSocket Routes ---


    router.route("/ws/create").handler(context -> {
      if (activeSessions.size() >= MAX_ACTIVE_SESSIONS) {
        context.response().setStatusCode(503).end("Server at capacity. Too many active sessions.");
        return;
      }
      String role = context.request().getParam("role");

      context.request().toWebSocket().onSuccess(webSocket -> {
        String tempSessionId;
        do {
          tempSessionId = generateRandomString(6);
        } while (activeSessions.containsKey(tempSessionId));
        final String sessionId = tempSessionId;
        String sessionPassword = generateRandomString(8);

        SessionData session = new SessionData(sessionId, sessionPassword);

        activeSessions.put(sessionId, session);
        System.out.println("Session CREATED [" + role + "]. ID: " + sessionId);

        session.timerId = vertx.setTimer(SESSION_TIMEOUT_MS, id -> {
          System.out.println("Session " + sessionId + " timed out (no counterpart connected).");
          destroySession(session, null, sessionId);
        });


        JsonObject initMessage = new JsonObject()
          .put("id", sessionId)
          .put("password", sessionPassword);
        webSocket.writeTextMessage(initMessage.encode());

        if ("sender".equalsIgnoreCase(role)) {
          setupSenderHandlers(session, webSocket);
        } else {
          setupReceiverHandlers(session, webSocket);
        }

        webSocket.closeHandler(v -> {
          System.out.println("Session " + sessionId + " closed.");
          destroySession(session, webSocket, sessionId);
        });
      }).onFailure(error -> System.err.println("WebSocket upgrade failed: " + error.getMessage()));
    });


    router.route("/ws/join/:sessionId").handler(context -> {
      String sessionId = context.pathParam("sessionId");
      String role = context.request().getParam("role");
      
      String pwd = context.request().getHeader("Authorization");
      if (pwd != null && pwd.startsWith("Bearer ")) {
        pwd = pwd.substring(7);
      } else {
        pwd = context.request().getParam("pwd"); // Fallback for testing/HTTP senders
      }

      SessionData session = activeSessions.get(sessionId);
      if (session == null || !MessageDigest.isEqual(session.password.getBytes(), pwd != null ? pwd.getBytes() : new byte[0])) {
        context.response().setStatusCode(401).end("Unauthorized or session not found.");
        return;
      }

      context.request().toWebSocket().onSuccess(webSocket -> {
        System.out.println("Session JOINED [" + role + "]. ID: " + sessionId);


        if (session.timerId != -1) {
          vertx.cancelTimer(session.timerId);
          session.timerId = -1;
        }

        if ("receiver".equalsIgnoreCase(role)) {
          if (session.senderMetaReceived || session.senderCtx != null || session.receiverRes != null) {
            webSocket.writeTextMessage(new JsonObject()
              .put("status", "ready").put("filename", session.fileName).encode());
          }
          setupReceiverHandlers(session, webSocket);
        } else if ("sender".equalsIgnoreCase(role)) {
          setupSenderHandlers(session, webSocket);
        }

        webSocket.closeHandler(v -> {
          System.out.println("Session " + sessionId + " closed.");
          destroySession(session, webSocket, sessionId);
        });
      });
    });

    // --- HTTP Routes ---

    router.get("/").handler(ctx -> {
      String host = ctx.request().getHeader("Host");
      if (host == null) host = "tubo.endlessite.com";
      String help = ASCII_LOGO +
        "Usage without installing anything:\n" +
        "  Send:    curl -sL https://" + host + "/run | sh -s send <file> <token>\n" +
        "  Receive: curl -sL https://" + host + "/run | sh -s receive <token>\n\n" +
        "Install the Go Client:\n" +
        "  curl -sL https://" + host + "/get | sh\n\n" +
        "Project: https://github.com/endlessite/tubo\n";
      ctx.response().putHeader("Content-Type", "text/plain").end(help);
    });


    router.get("/run").handler(ctx -> {
      ctx.response().putHeader("Content-Type", "text/plain").sendFile("run.sh");
    });
    router.get("/get").handler(ctx -> {
      ctx.response().putHeader("Content-Type", "text/plain").sendFile("install.sh");
    });


    router.get("/:sessionId").handler(this::handleCurlGet);
    router.get("/:sessionId/:filename").handler(this::handleCurlGet);


    router.post("/:sessionId").handler(this::handleCurlPost);
    router.post("/:sessionId/:filename").handler(this::handleCurlPost);


    int port = Integer.parseInt(System.getenv() != null && System.getenv().containsKey("PORT")
      ? System.getenv("PORT") : "8080");

    return vertx.createHttpServer(
        new io.vertx.core.http.HttpServerOptions()
          .setIdleTimeout(10)
          .setIdleTimeoutUnit(TimeUnit.MINUTES)
      )
      .requestHandler(router)
      .listen(port)
      .onSuccess(server -> System.out.println("Relay Server listening on port " + port))
      .onFailure(err -> System.err.println("Failed to start server: " + err.getMessage()));
  }

  // GET /:id — curl receiver
  private void handleCurlGet(RoutingContext context) {
    String sessionId = context.pathParam("sessionId");
    SessionData session = activeSessions.get(sessionId);

    if (session == null) {
      context.response().setStatusCode(404).end("Session not found.\n");
      return;
    }

    if (!validateBasicAuth(context, session)) return;


    if (session.timerId != -1) {
      vertx.cancelTimer(session.timerId);
      session.timerId = -1;
    }


    session.receiverRes = context.response();
    context.response().setChunked(true);
    context.response().putHeader("Content-Disposition", "attachment; filename=\"" + session.fileName + "\"");
    context.response().putHeader("Content-Type", "application/octet-stream");

    // HTTP GET starts immediately
    if (session.senderWs != null) {
      session.senderWs.writeTextMessage(new JsonObject().put("status", "start").encode());
    }
    if (session.onStartCallback != null) {
      session.onStartCallback.run();
    }

    context.request().connection().closeHandler(v -> {
      destroySession(session, null, sessionId);
    });
  }

  // POST /:id — curl sender
  private void handleCurlPost(RoutingContext context) {
    String sessionId = context.pathParam("sessionId");
    String pathFileName = context.pathParam("filename");
    SessionData session = activeSessions.get(sessionId);

    if (session == null) {
      context.response().setStatusCode(404).end("Session not found.\n");
      return;
    }

    if (!validateBasicAuth(context, session)) return;


    if (session.timerId != -1) {
      vertx.cancelTimer(session.timerId);
      session.timerId = -1;
    }

    // Determine filename from path or header
    if (pathFileName != null) {
      session.fileName = pathFileName.replaceAll("[\"\\r\\n]", "_");
    } else {
      String xFileName = context.request().getHeader("X-File-Name");
      if (xFileName != null) session.fileName = xFileName.replaceAll("[\"\\r\\n]", "_");
    }

    // Track sender context to prevent POST connection leak if receiver disconnects
    session.senderCtx = context;

    // Pause stream until receiver starts
    context.request().pause();
    session.onStartCallback = () -> context.request().resume();


    if (session.receiverWs != null) {
      JsonObject readyMsg = new JsonObject().put("status", "ready").put("filename", session.fileName);
      session.receiverWs.writeTextMessage(readyMsg.encode());
    } else if (session.receiverRes != null) {
      // Start immediately if both sides are HTTP
      session.onStartCallback.run();
    }


    context.request().handler(buffer -> {
      if (session.receiverWs != null) {
        session.receiverWs.writeBinaryMessage(buffer);
        if (session.receiverWs.writeQueueFull()) {
          context.request().pause();
          session.receiverWs.drainHandler(v -> context.request().resume());
        }
      } else if (session.receiverRes != null) {
        session.receiverRes.write(buffer);
        if (session.receiverRes.writeQueueFull()) {
          context.request().pause();
          session.receiverRes.drainHandler(v -> context.request().resume());
        }
      }
    });

    context.request().endHandler(v -> {
      context.response().setStatusCode(200).end("Transfer complete.\n");
      destroySession(session, null, sessionId);
    });

    context.request().connection().closeHandler(v -> {
      destroySession(session, null, sessionId);
    });
  }


  private boolean validateBasicAuth(RoutingContext context, SessionData session) {
    String authHeader = context.request().getHeader("Authorization");
    if (authHeader == null || !authHeader.startsWith("Basic ")) {
      context.response().putHeader("WWW-Authenticate", "Basic realm=\"Tubo\"");
      context.response().setStatusCode(401).end("Unauthorized\n");
      return false;
    }

    try {
      String base64 = authHeader.substring(6);
      String[] parts = new String(Base64.getDecoder().decode(base64)).split(":", 2);
      if (parts.length != 2 || !MessageDigest.isEqual(session.password.getBytes(), parts[1].getBytes())) {
        context.response().setStatusCode(401).end("Unauthorized\n");
        return false;
      }
      return true;
    } catch (Exception e) {
      context.response().setStatusCode(400).end("Bad Request\n");
      return false;
    }
  }
}
