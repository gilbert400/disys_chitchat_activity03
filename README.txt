To run the program with 3 clients follow these steps.

1. run the following command in the terminal:
	go run -id=0 -n=3

2. open a new terminal and run:
	go run -id=1 -n=3

3. open a third terminal and run:
	go run -id=2 -n=3


If you need more than 3 nodes change the n to the number of nodes wanted,
and repeat step 3 (with a new id in a new terminal). 

It is well noted that our implementation only works on localhost.
