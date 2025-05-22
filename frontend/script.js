// Wait for the DOM to be fully loaded before accessing elements
document.addEventListener("DOMContentLoaded", () => {
  const newSessionButton = document.getElementById("newSessionButton");

  if (newSessionButton) {
    newSessionButton.addEventListener("click", async () => {
      try {
        // Make a request to the backend to create a new session
        // Assuming your backend has an endpoint like /new-session that returns a JSON object with a 'sessionId' field
        const response = await fetch("/new-session", {
          method: "POST", // Or GET, depending on your backend design
          headers: {
            "Content-Type": "application/json",
          },
          // body: JSON.stringify({}) // Send body if needed by backend
        });

        if (response.status >= 200 && response.status < 300) {
          const data = await response.json();
          const sessionId = data.sessionId; // Assuming the backend returns { "sessionId": "some-uuid" }

          if (sessionId) {
            // Redirect to the new session URL
            window.location.href = `/s/${sessionId}`; // Using /s/<uuid> format
          } else {
            console.error("Backend did not return a sessionId.");
            // Optionally display an error message
          }
        } else {
          console.error(
            "Failed to create new session:",
            response.status,
            response.statusText,
          );
          // Optionally display an error message to the user
          return;
        }
      } catch (error) {
        console.error("Error creating new session:", error);
        // Optionally display an error message to the user
      }
    });
  } else {
    console.error(
      "Error: Element with ID 'newSessionButton' not found in the DOM.",
    );
  }
});
