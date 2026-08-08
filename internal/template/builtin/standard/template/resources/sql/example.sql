SELECT category, categoryIndex, operation,
        SUM(pp1) AS pp1, SUM(ac1) AS ac1
FROM new_cities
WHERE date = (SELECT max(date) FROM new_cities)
GROUP BY category, categoryIndex, operation
ORDER BY categoryIndex