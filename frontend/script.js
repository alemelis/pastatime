const timerElement = document.getElementById("timer");
const lapTimeDisplayElement = document.getElementById("lapTimeDisplay");
const lapHistoryElement = document.getElementById("lapHistory");
const controllerElement = document.getElementById("controller");
const clientNameDisplayElement = document.getElementById("clientNameDisplay"); // Addded client name
const startButton = document.getElementById("start");
const pauseButton = document.getElementById("pause");
const resetButton = document.getElementById("reset");
const nextButton = document.getElementById("next");
const socket = new WebSocket("ws://localhost:8080/ws");

let currentTime = 0;
let yourId = null;

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

    // Update timer display with animation
    if (typeof anime !== "undefined") {
      anime({
        targets: { val: currentTime },
        val: newTime,
        duration: 100,
        easing: "linear",
        update: (anim) => {
          currentTime = anim.animations[0].currentValue;
          timerElement.textContent = (currentTime / 1000).toFixed(1);
        },
      });
    } else {
      currentTime = newTime;
      timerElement.textContent = (currentTime / 1000).toFixed(1);
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
        console.log("Lap object:", lap);
        historyHTML += `<li>${lap.client}: ${(lap.timeMs / 1000).toFixed(1)} s</li>`;
      });
    } else {
      historyHTML += "<li>No laps yet</li>";
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

// Disable buttons initially
startButton.disabled = true;
pauseButton.disabled = true;
resetButton.disabled = true;
nextButton.disabled = true;
