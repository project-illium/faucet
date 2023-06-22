Vue.component('card', {
	props: ['data'],
	template: `
    <div class="card">
      <div class="card-content">
        <div class="card-header">
        	<p><strong>BlockID:</strong> <span>{{ data.Header.blockID }}</span></p>
  			<p><strong>Producer:</strong> <span>{{ data.Header.producerID }}</span></p>
  			<p><strong>Height:</strong> <span>{{ data.Header.height }}</span></p>
  		</div>
        <div class="card-body">
          <h3>Txids:</h3>
          <ul>
            <li v-for="txid in data.Txids" :key="txid">{{ txid }}</li>
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
				{
					Header: {
						blockID: "9cc3d50110b00cf51ea9afc64514c506d890367976d00276e1a7f012896017c1",
						version: 1,
						height: 1,
						parent: "9610886dceeed030ab6afb960343fc36ce8bfab58387f92ca20864c61f8da1a0",
						timestamp: 1687370889,
						txRoot: "7f3e7f1ad8b3129fa7d61e9e4cfe23b56e6f8acb2e2ddaaa381a35ee4072b398",
						producerID: "12D3KooWN2RRWUokkcCjrf8zypvHwGv2u6rUepFAXheambSst5fV",
						signature: "fa5dc41470d7e51165697b274ca75b82e6e8a02fc1196fa71a86b6ebeecd8208abd55b25d5a9f902c49199710ea0fe72738ae968e7d496f181a10fe302783a04"
					},
					Txids: [
						"ef72632c296f60665dfc8a2ac74804f27f39eaf9882139fafcd2c7038914e37c"
					]
				}
			],
			receivedCards: []
		};
	},
	mounted() {
		//this.fetchInitialCards();
		//this.setupWebTransport();
	},
	methods: {
		fetchInitialCards() {
			// Fetch initial cards from the server
			// Adjust the URL and handling as per your server implementation
			fetch('/api/cards')
				.then(response => response.json())
				.then(data => {
					this.cards = data;
				})
				.catch(error => {
					console.error('Error fetching initial cards:', error);
				});
		},
		setupWebTransport() {
			// Set up WebTransport connection
			const webTransport = new WebTransport('wss://your-server-url/webtransport');
			webTransport.onmessage = event => {
				const card = JSON.parse(event.data);
				this.receivedCards.unshift(card); // Put the new card at the top of the list
			};
			webTransport.connect()
				.then(() => {
					console.log('WebTransport connected');
				})
				.catch(error => {
					console.error('WebTransport connection error:', error);
				});
		},
		loadMoreCards() {
			// Fetch additional cards from the server
			// Adjust the URL and handling as per your server implementation
			fetch('/api/more-cards')
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
