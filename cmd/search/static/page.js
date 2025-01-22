class RequestManager {
    constructor() {
        this.currentController = null;
    }

    async makeRequest(url, options={}) {
        try {
            if (this.currentController) {
                this.currentController.abort();
            }

            this.currentController = new AbortController();

            const fetchOptions = {
                ...options,
                signal: this.currentController.signal
            };

            const response = await fetch(url, fetchOptions);
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }

            const data = await response.json();
            return data;
        } catch (error) {
            if (error.name === 'AbortError') {
                console.log('request cancelled');
                return null;
            }
            throw error;
        } finally {
            this.currentController = null;
        }
    }
}

// Get DOM elements
const searchInput = document.getElementById('searchInput');
const resultsContainer = document.getElementById('resultsContainer');
const requestManager = new RequestManager();

searchInput.addEventListener("input", async (event) => {
    try {
        const text = event.target.value;
        if (text.length >= 3) {
            const data = await requestManager.makeRequest(
                `/prefix?q=${text}`,
                {
                    method: 'GET',
                    headers: {
                        'Content-Type': 'application/json'
                    }
                }
            );

            if (data) {
                console.log('Prefix results: ', data);
            }
        }
    } catch (error) {
        console.error('Error fetching search results: ', error);
    }
});

function handleSearch() {
    runQuery(searchInput.value.trim());
}

function handleKeyUp(event) {
    if (event.code === "Enter") {
        runQuery(searchInput.value.trim());
    }
}

function runQuery(query) {
    if (query) {
        fetch(`/search?q=${encodeURIComponent(query)}`)
        .then((response) => {
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            return response.text();
        })
        .then((html) => { resultsContainer.innerHTML = html; })
        .catch(function (error) { console.error('Error fetching search results: ', error); })
    }
}