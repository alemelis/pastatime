// Function to generate Unicode loading bar (kept outside DOMContentLoaded as it's a pure function)
const barLength = 40; // Length of the Unicode loading bar
const generateUnicodeBar = (percentage) => {
  const filledLength = Math.round((percentage / 100) * barLength);
  const emptyLength = barLength - filledLength;
  // Using Unicode block elements for a nicer look
  const filledBar = "█".repeat(filledLength);
  const emptyBar = "░".repeat(emptyLength); // Using light shade character for empty part
  return `${filledBar}${emptyBar} ${percentage.toFixed(1)}%`;
};

// Wait for the DOM to be fully loaded before accessing elements
document.addEventListener("DOMContentLoaded", () => {
  console.log(
    "DOMContentLoaded fired on session page. Attempting to get elements...",
  ); // Log to check if this runs

  const timerElement = document.getElementById("timer");
  const lapTimeDisplayElement = document.getElementById("lapTimeDisplay"); // This element no longer exists, but the variable is still here
  const lapHistoryElement = document.getElementById("lapHistory");
  const controllerElement = document.getElementById("controller");
  const clientNameDisplayElement = document.getElementById("clientNameDisplay"); // Added client name
  const startButton = document.getElementById("start");
  const pauseButton = document.getElementById("pause");
  const resetButton = document.getElementById("reset");
  const nextButton = document.getElementById("next");
  const asciiLoadingBarElement = document.getElementById("asciiLoadingBar"); // Get the ASCII loading bar element
  const clientListElement = document.getElementById("clientList"); // Get the client list element

  // Extract session ID from the URL
  const pathSegments = window.location.pathname.split("/");
  const sessionId = pathSegments[2]; // Assuming URL format is /s/{sessionId}

  // Connect to the WebSocket endpoint for this specific session
  const socketUrl = `ws://localhost:8080/s/${sessionId}/ws`;
  const socket = new WebSocket(socketUrl);

  // Check if the loading bar element was found
  if (!asciiLoadingBarElement) {
    console.error(
      "Error: Element with ID 'asciiLoadingBar' not found in the DOM.",
    );
  }
  // Check if the client list element was found
  if (!clientListElement) {
    console.error("Error: Element with ID 'clientList' not found in the DOM.");
  }

  let currentTime = 0;
  let yourId = null;
  const oneMinuteInMs = 60000; // 1 minute in milliseconds
  const totalLoadingTime = oneMinuteInMs; // The time it takes for the loading bar to fill

  socket.onmessage = (event) => {
    let msg = {};
    try {
      msg = JSON.parse(event.data);
    } catch (err) {
      console.error("Bad JSON:", event.data);
      return;
    }

    if (msg.type === "update") {
      const newTime = msg.time;
      const lapTime = msg.lapTime; // Still exists in msg, but not used
      const lastLapClient = msg.lastLapClient; // Still exists in msg, but not used
      const lapHistory = msg.lapHistory;
      const activeClient = msg.activeClient;
      const clients = msg.clients; // Get the list of clients
      yourId = msg.yourId;

      // Update client name display
      if (clientNameDisplayElement) {
        // Added check
        clientNameDisplayElement.textContent = `You are: ${yourId}`;
      }

      // Update connected clients list - Sort alphabetically
      if (clients && Array.isArray(clients) && clientListElement) {
        // Added check for clientListElement
        clientListElement.innerHTML = ""; // Clear the current list
        const sortedClients = [...clients].sort(); // Create a copy and sort it
        sortedClients.forEach((client) => {
          const li = document.createElement("li");
          li.textContent = client;
          // Highlight the active client
          if (client === activeClient) {
            li.style.fontWeight = "bold";
            li.style.color = "#006400"; // Dark green for active client
          }
          // Highlight your own ID
          if (client === yourId && client !== activeClient) {
            li.style.color = "#4b0082"; // Indigo for your ID if not active
          } else if (client === yourId && client === activeClient) {
            // If you are also the active client, the active client style takes precedence
          }
          clientListElement.appendChild(li);
        });
      }

      // Calculate loading percentage
      const loadingPercentage = Math.min(newTime / totalLoadingTime, 1) * 100;

      // Update Unicode loading bar
      if (asciiLoadingBarElement) {
        // This check should prevent the error
        asciiLoadingBarElement.textContent =
          generateUnicodeBar(loadingPercentage);
      } else {
        console.error("asciiLoadingBarElement is null in onmessage."); // Log if it's null here
      }

      // Update timer text and color
      if (typeof anime !== "undefined") {
        anime({
          targets: { val: currentTime },
          val: newTime,
          duration: 100,
          easing: "linear",
          update: (anim) => {
            currentTime = anim.animations[0].currentValue;
            if (timerElement) {
              // Added check
              timerElement.textContent = (currentTime / 1000).toFixed(1);
            }

            // Change timer color based on time
            if (timerElement) {
              // Added check
              if (currentTime >= oneMinuteInMs) {
                timerElement.classList.remove("timer-green");
                timerElement.classList.add("timer-red");
              } else {
                timerElement.classList.remove("timer-red");
                timerElement.classList.add("timer-green");
              }
            }
          },
        });
      } else {
        // Fallback if animejs is not loaded
        currentTime = newTime;
        if (timerElement) {
          // Added check
          timerElement.textContent = (currentTime / 1000).toFixed(1);

          // Change timer color based on time (fallback)
          if (currentTime >= oneMinuteInMs) {
            timerElement.style.color = "#8b0000"; // Dark red
          } else {
            timerElement.style.color = "#006400"; // Dark green
          }
        }
      }

      // Update lap history display
      let historyHTML = "<ul>";
      if (lapHistory && lapHistory.length > 0) {
        lapHistory.forEach((lap) => {
          historyHTML += `<li>${lap.client}: ${(lap.timeMs / 1000).toFixed(1)} s</li>`;
        });
      } else {
        historyHTML += "<li>No standups yet</li>";
      }
      historyHTML += "</ul>";
      if (lapHistoryElement) {
        // Added check
        lapHistoryElement.innerHTML = historyHTML;
      }

      // Update controller display and button states
      if (activeClient) {
        if (controllerElement) {
          controllerElement.textContent = `Controller: ${activeClient}`;
        }
        const isYou = yourId === activeClient;
        if (startButton) startButton.disabled = !isYou;
        if (pauseButton) pauseButton.disabled = !isYou;
        if (resetButton) resetButton.disabled = !isYou;
        if (nextButton) nextButton.disabled = !isYou;
      } else {
        if (controllerElement) {
          controllerElement.textContent = "No active controller";
        }
        if (startButton) startButton.disabled = true;
        if (pauseButton) pauseButton.disabled = true;
        if (resetButton) resetButton.disabled = true;
        if (nextButton) nextButton.disabled = true;
      }
    }
  };

  const sendCommand = (cmd) => {
    // Check if buttons exist before checking disabled property
    if ((startButton && !startButton.disabled) || cmd === "next") {
      socket.send(JSON.stringify({ type: "command", command: cmd }));
    } else {
      console.log("Not the active controller.");
    }
  };

  // Add event listeners only after buttons are confirmed to exist
  if (startButton) startButton.onclick = () => sendCommand("start");
  if (pauseButton) pauseButton.onclick = () => sendCommand("pause");
  if (resetButton) resetButton.onclick = () => sendCommand("reset");
  if (nextButton) nextButton.onclick = () => sendCommand("next");

  // Disable buttons initially and set initial timer color
  if (startButton) startButton.disabled = true;
  if (pauseButton) pauseButton.disabled = true;
  if (resetButton) resetButton.disabled = true;
  if (nextButton) nextButton.disabled = true;
  // Set initial timer color to green
  if (timerElement) {
    // Added check
    timerElement.classList.add("timer-green");
  }
}); // End of DOMContentLoaded listener
