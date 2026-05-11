math.randomseed(os.time())
request = function()
    local path = "/test/" .. math.random(1, 1000000)
    return wrk.format("GET", path)
end