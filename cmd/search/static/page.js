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
const suggestionsDropDown = document.getElementById('suggestionsDropdown');
const suggestionsList = document.getElementById('suggestionsList');

let currentSuggestionIndex = -1;

searchInput.addEventListener('input', async (event) => {
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
                updateSuggestions(data.matches);
            }
        }
        if (text.length == 0) {
            clearSuggestions();
        }
    } catch (error) {
        console.error('Error fetching search results: ', error);
    }
});

searchInput.addEventListener('keydown', function(e) {
    switch(e.key) {
        case 'ArrowDown':
            e.preventDefault();
            currentSuggestionIndex = currentSuggestionIndex + 1;
            if (currentSuggestionIndex > (suggestionsList.children.length-1)) {
                currentSuggestionIndex = 0;
            }
            updateHighlight();
            break
        case 'ArrowUp':
            e.preventDefault();
            currentSuggestionIndex = currentSuggestionIndex - 1;
            if (currentSuggestionIndex < 0) {
                currentSuggestionIndex = suggestionsList.children.length-1;
            }
            updateHighlight();
            break
        case 'Escape':
            clearSuggestions();
            break
        case 'Enter':
            if (currentSuggestionIndex !== -1) {
                selectSuggestion(suggestionsList.children[currentSuggestionIndex]);
            }
            break
    }
});

function handleSearch() {
    runQuery(searchInput.value.trim());
}

function handleKeyUp(event) {
    if (event.code === 'Enter') {
        runQuery(searchInput.value.trim());
    }
}

function clearSuggestions() {
    updateSuggestions([]);
}

function selectSuggestion(suggestionElement) {
    searchInput.value = suggestionElement.textContent;

    clearSuggestions();

    handleSearch();
}

function updateHighlight() {
    const suggestions = suggestionsList.children;

    for (let suggestion of suggestions) {
        suggestion.classList.remove('bg-gray-200');
    }

    if (currentSuggestionIndex !== -1) {
        suggestions[currentSuggestionIndex].classList.add('bg-gray-200');
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

function updateSuggestions(suggestions) {
    suggestionsList.innerHTML = '';

    if (suggestions.length === 0) {
        suggestionsDropDown.classList.add('hidden');
        currentSuggestionIndex = -1;
        return;
    }

    suggestions.forEach((suggestion, index) => {
        const li = document.createElement('li');
        li.textContent = suggestion;
        li.className = 'px-4 py-2 hover:bg-gray-100 cursor-pointer';

        li.addEventListener('click', () => { selectSuggestion(li) });

        suggestionsList.appendChild(li);
    });

    suggestionsDropDown.classList.remove('hidden');

    currentSuggestionIndex = -1;
}