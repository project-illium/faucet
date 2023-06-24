Vue.component('card', {
	props: ['data'],
	template: `
    <div class="card">
      <div class="card-content">
        <div class="card-header">
        	<p><strong>BlockID:</strong> <span>{{ data.blockID }}</span></p>
  			<p><strong>Producer:</strong> <span>{{ data.producerID }}</span></p>
  			<p><strong>Height:</strong> <span>{{ data.height }}</span></p>
  		</div>
        <div class="card-body">
          <h3>Txids:</h3>
          <ul>
            <li v-for="txid in data.txids" :key="txid">{{ txid }}</li>
          </ul>
        </div>
      </div>
    </div>
  `
});

new Vue({
	el: '#app',
	data() {
		return {
			cards: [
			],
			receivedCards: []
		};
	},
	mounted() {
		this.fetchInitialCards();
		this.setupWebSocket();
		window.addEventListener('scroll', this.handleScroll);
	},
	beforeDestroy() {
		window.removeEventListener('scroll', this.handleScroll);
	},
	methods: {
		fetchInitialCards() {
			// Fetch initial cards from the server
			// Adjust the URL and handling as per your server implementation
			fetch('/blocks/-1')
				.then(response => response.json())
				.then(data => {
					this.cards = data;
				})
				.catch(error => {
					console.error('Error fetching initial cards:', error);
				});
		},
		setupWebSocket() {
			// Check if WebSocket is supported
			if (!("WebSocket" in window)) {
				console.error("WebSocket is not supported in this browser.");
				return;
			}

			// Set up WebSocket connection
			const websocket = new WebSocket("wss://faucet.illium.org/ws"); // Adjust the URL and port to your WebSocket server

			// When a message is received
			websocket.onmessage = event => {
				const card = JSON.parse(event.data);
				this.receivedCards.unshift(card); // Put the new card at the top of the list
			};

			// Handle errors
			websocket.onerror = error => {
				console.error("WebSocket Error: ", error);
			};

			// When the connection is closed
			websocket.onclose = event => {
				console.log("WebSocket Connection Closed", event);
			};
		},
		handleScroll() {
			const { scrollTop, clientHeight, scrollHeight } = document.documentElement;
			const bottomOffset = 20;

			if (scrollTop + clientHeight >= scrollHeight - bottomOffset) {
				this.loadMoreCards();
			}
		},
		loadMoreCards() {
			if (this.cards.length === 0) {
				return;
			}

			const lastCard = this.cards[this.cards.length - 1];
			const fromHeight = lastCard.height;
			const url = `/blocks/${fromHeight}`;
			fetch(url)
				.then(response => response.json())
				.then(data => {
					this.cards = this.cards.concat(data);
				})
				.catch(error => {
					console.error('Error fetching more cards:', error);
				});
		}
	},
	watch: {
		receivedCards(newCards) {
			// Prepend newly received cards to the top of the list
			this.cards = newCards.concat(this.cards);
		}
	}
})

document.getElementById('get-coins-form').addEventListener('submit', function(e) {
	e.preventDefault(); // Prevent the default form submission

	const input = document.getElementById('input').value;

	fetch('https://faucet.illium.org/getcoins', {
		method: 'POST',
		headers: {
			'Content-Type': 'application/json'
		},
		body: JSON.stringify({
			addr: input
		})
	})
		.then(response => response.json())
		.then(data => {
			console.log(data);
			document.getElementById('input').value = ""; // Clear the input field after form submission
		})
		.catch((error) => {
			console.error('Error:', error);
		});
});