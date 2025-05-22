const timerElement = document.getElementById("timer");
const lapTimeDisplayElement = document.getElementById("lapTimeDisplay");
const lapHistoryElement = document.getElementById("lapHistory");
const controllerElement = document.getElementById("controller");
const clientNameDisplayElement = document.getElementById("clientNameDisplay"); // Added client name
const startButton = document.getElementById("start");
const pauseButton = document.getElementById("pause");
const resetButton = document.getElementById("reset");
const nextButton = document.getElementById("next");
const asciiLoadingBarElement = document.getElementById("asciiLoadingBar"); // Get the ASCII loading bar element
const socket = new WebSocket("ws://localhost:8080/ws");

let currentTime = 0;
let yourId = null;
const oneMinuteInMs = 60000; // 1 minute in milliseconds
const totalLoadingTime = oneMinuteInMs; // The time it takes for the loading bar to fill
const barLength = 40; // Reduced bar length slightly for better fit in the pill

// Function to generate Unicode loading bar
const generateUnicodeBar = (percentage) => {
  const filledLength = Math.round((percentage / 100) * barLength);
  const emptyLength = barLength - filledLength;
  // Using Unicode block elements for a nicer look
  const filledBar = "█".repeat(filledLength);
  const emptyBar = "░".repeat(emptyLength); // Using light shade character for empty part
  return `${filledBar}${emptyBar} ${percentage.toFixed(1)}%`;
};

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
    const lapTime = msg.lapTime;
    const lastLapClient = msg.lastLapClient;
    const lapHistory = msg.lapHistory;
    const activeClient = msg.activeClient;
    yourId = msg.yourId;

    // Update client name display
    clientNameDisplayElement.textContent = `You are: ${yourId}`;

    // Calculate loading percentage
    const loadingPercentage = Math.min(newTime / totalLoadingTime, 1) * 100;

    // Update Unicode loading bar
    asciiLoadingBarElement.textContent = generateUnicodeBar(loadingPercentage);

    // Update timer text and color
    if (typeof anime !== "undefined") {
      anime({
        targets: { val: currentTime },
        val: newTime,
        duration: 100,
        easing: "linear",
        update: (anim) => {
          currentTime = anim.animations[0].currentValue;
          timerElement.textContent = (currentTime / 1000).toFixed(1);

          // Change timer color based on time
          if (currentTime >= oneMinuteInMs) {
            timerElement.classList.remove("timer-green");
            timerElement.classList.add("timer-red");
          } else {
            timerElement.classList.remove("timer-red");
            timerElement.classList.add("timer-green");
          }
        },
      });
    } else {
      // Fallback if animejs is not loaded
      currentTime = newTime;
      timerElement.textContent = (currentTime / 1000).toFixed(1);

      // Change timer color based on time (fallback)
      if (currentTime >= oneMinuteInMs) {
        timerElement.style.color = "#8b0000"; // Dark red
      } else {
        timerElement.style.color = "#006400"; // Dark green
      }
    }

    // Update lap time display with client name
    if (lapTime > 0 && lastLapClient) {
      lapTimeDisplayElement.textContent = ""; //`Lap (${lastLapClient}): ${(lapTime / 1000).toFixed(1)}`;
    } else {
      lapTimeDisplayElement.textContent = "";
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
    lapHistoryElement.innerHTML = historyHTML;

    // Update controller display and button states
    if (activeClient) {
      controllerElement.textContent = `Controller: ${activeClient}`;
      const isYou = yourId === activeClient;
      startButton.disabled = !isYou;
      pauseButton.disabled = !isYou;
      resetButton.disabled = !isYou;
      nextButton.disabled = !isYou;
    } else {
      controllerElement.textContent = "No active controller";
      startButton.disabled = true;
      pauseButton.disabled = true;
      resetButton.disabled = true;
      nextButton.disabled = true;
    }
  }
};

const sendCommand = (cmd) => {
  if (!startButton.disabled || cmd === "next") {
    socket.send(JSON.stringify({ type: "command", command: cmd }));
  } else {
    console.log("Not the active controller.");
  }
};

startButton.onclick = () => sendCommand("start");
pauseButton.onclick = () => sendCommand("pause");
resetButton.onclick = () => sendCommand("reset");
nextButton.onclick = () => sendCommand("next");

// Disable buttons initially and set initial timer color
startButton.disabled = true;
pauseButton.disabled = true;
resetButton.disabled = true;
nextButton.disabled = true;
// Set initial timer color to green
timerElement.classList.add("timer-green");
