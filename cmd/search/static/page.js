// Get DOM elements
const searchInput = document.getElementById('searchInput');
const resultsContainer = document.getElementById('resultsContainer');

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
        fetch(`/search?query=${encodeURIComponent(query)}`)
        .then((response) => { return response.text(); })
        .then((html) => { resultsContainer.innerHTML = html; })
        .catch(function (err) { console.warn("Something went wrong", err); })
    }
}