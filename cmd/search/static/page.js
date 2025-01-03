// Get DOM elements
const searchInput = document.getElementById('searchInput');
const resultsContainer = document.getElementById('resultsContainer');

function handleSearch() {
    runQuery();
}

function handleKeyUp(event) {
    if (event.code === "Enter") {
        runQuery();
    }
}

function runQuery() {
    const query = searchInput.value.trim();
    if (query) {
        fetch(`/search?query=${encodeURIComponent(query)}`)
        .then((response) => { return response.text(); })
        .then((html) => { resultsContainer.innerHTML = html; })
        .catch(function (err) { console.warn("Something went wrong", err); })
    }
}